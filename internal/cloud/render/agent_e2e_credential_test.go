package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/agent"
	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// TestCredentialFilesConvergeWithRealAgent 覆盖凭据落点的整条链路：
// 云端 file 条目 → 首次接管归档 → 真实代理落盘 → 第二轮无差异。
//
// 私钥 CRLF 归一、托管块只算块内哈希这两处一旦两侧口径不一致，
// 第二轮就会再出 batch，就是 2026-10-23 那类每轮重写的事故。
func TestCredentialFilesConvergeWithRealAgent(t *testing.T) {
	e := newTestEnv(t)
	author := ssot.Author{Name: "dev-1", Source: "mcp"}
	for name, value := range map[string]string{
		"ssh-dec-a": "-----BEGIN OPENSSH PRIVATE KEY-----\r\nAAAA\r\n-----END OPENSSH PRIVATE KEY-----",
		"proj-env":  "TOKEN=abc\n",
	} {
		if _, err := e.assets.PutSecret(e.key, assets.PutSecretInput{Name: name, Value: value, Author: author}); err != nil {
			t.Fatalf("PutSecret: %v", err)
		}
	}
	e.writeBundle(t, "creds", []assets.AssetFile{
		{Path: "ssh/dec_a", Content: []byte("{{secret:ssh-dec-a}}")},
		{Path: "ssh/dec_a.pub", Content: []byte("ssh-ed25519 AAAA dec\n")},
		{Path: "ssh/dec.conf", Content: []byte("Host github.com\n  IdentityFile ~/.ssh/dec_a\n")},
		{Path: "ssh/config", Content: []byte("Include config.d/dec.conf\r\n")},
		{Path: "proj/app.env", Content: []byte("{{secret:proj-env}}")},
	}, `name: creds
items:
  - kind: file
    path: ssh/dec_a
    name: key
    planes: [user]
    root: ssh
    target: dec_a
  - kind: file
    path: ssh/dec_a.pub
    name: pub
    planes: [user]
    root: ssh
    target: dec_a.pub
  - kind: file
    path: ssh/dec.conf
    name: frag
    planes: [user]
    root: ssh
    target: config.d/dec.conf
  - kind: file
    path: ssh/config
    name: cfg
    planes: [user]
    root: ssh
    target: config
    format: managed_block
  - kind: file
    path: proj/app.env
    name: env
    planes: [project]
    root: secrets
    target: app.env
`)
	project := filepath.Join(e.home, "workspace", "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	d := ssot.Device{ID: "dev-1", Name: "测试机", Kind: ssot.DeviceMine, OS: "darwin", Arch: "arm64", UserBundles: []string{"creds"}}
	e.registerDevice(t, d)
	if _, err := e.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.ProjectTable](tx.Store(), ssot.FileProjects)
		if err != nil {
			return "", err
		}
		table.Projects = append(table.Projects, ssot.Project{ID: "p1", DeviceID: d.ID, Name: "proj", Path: project, Bundles: []string{"creds"}})
		return "登记项目", tx.WriteJSON(ssot.FileProjects, table)
	}); err != nil {
		t.Fatal(err)
	}

	// 现场已有旧系统留下的私钥（内容不同）和用户自己的 config。
	sshDir := filepath.Join(e.home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "dec_a"), []byte("old-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	userCfg := "Host git.woa.com\n  User git\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(userCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	state := t.TempDir()
	writer := agent.NewWriter(agent.NewResolver(e.home, []agent.Project{{ID: "p1", Path: project}}), e.home)
	writer.SetArchiveDir(state)
	roots := []string{proto.RootSSH, proto.RootSecrets}
	locals := []proto.LocalProject{{ID: "p1", Path: project}}

	first, err := e.render.Sync(d, proto.SyncRequest{Roots: roots, Projects: locals})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if first.Batch == nil || len(first.Batch.Files) != 5 {
		t.Fatalf("首次应下发 5 条，实际 %+v", first.Batch)
	}
	var acks []proto.AckResult
	var manifest []proto.ManifestEntry
	var notes []string
	for _, f := range first.Batch.Files {
		res, err := writer.Apply(f)
		if err != nil {
			t.Fatalf("代理落盘 %s 失败: %v", f.ID, err)
		}
		notes = append(notes, res.Warnings...)
		acks = append(acks, proto.AckResult{ID: f.ID, OK: true, Hash: res.Hash, Paths: res.Paths, Notes: res.Warnings})
		h, err := agent.HashFile(res.Paths[0], f.Format, f.Key)
		if err != nil {
			t.Fatalf("HashFile %s: %v", f.ID, err)
		}
		manifest = append(manifest, proto.ManifestEntry{ID: f.ID, Hash: h, Paths: res.Paths, Format: f.Format, Key: f.Key})
	}
	if err := e.render.States().RecordAck(d.ID, first.Batch.Revision, acks); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	adopted := 0
	for _, n := range notes {
		if strings.Contains(n, "已归档后覆盖") {
			adopted++
		}
	}
	if adopted != 1 {
		t.Fatalf("只有内容不同的私钥应被归档，实际 notes=%v", notes)
	}
	key, _ := os.ReadFile(filepath.Join(sshDir, "dec_a"))
	if string(key) != "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n" {
		t.Fatalf("私钥落盘错误: %q", key)
	}
	cfg, _ := os.ReadFile(filepath.Join(sshDir, "config"))
	if !strings.Contains(string(cfg), userCfg) || !strings.Contains(string(cfg), proto.BlockBegin) {
		t.Fatalf("config 应保留用户内容并插入托管块: %q", cfg)
	}

	second, err := e.render.Sync(d, proto.SyncRequest{Revision: first.Batch.Revision, Manifest: manifest, Roots: roots, Projects: locals})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if second.Batch != nil {
		for _, f := range second.Batch.Files {
			t.Errorf("第二轮仍下发 %s", f.ID)
		}
		t.Fatalf("真实落盘后第二轮应无差异，实际 files=%d deletes=%d", len(second.Batch.Files), len(second.Batch.Deletes))
	}
	if len(second.Overrides) != 0 {
		t.Fatalf("第二轮不应有覆盖记录: %+v", second.Overrides)
	}
}
