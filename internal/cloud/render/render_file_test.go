package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

var credRoots = []string{proto.RootSSH, proto.RootSecrets}

// seedCredBundle 写一个含私钥、公钥、config 托管块与项目 secret 的 bundle。
func seedCredBundle(t *testing.T, f *fixture) {
	t.Helper()
	f.putSecret(t, "ssh-dec-github", "-----BEGIN OPENSSH PRIVATE KEY-----\r\nAAAA\r\n-----END OPENSSH PRIVATE KEY-----")
	f.putSecret(t, "proj-env", "TOKEN=abc\n")
	f.writeBundle(t, "creds", []assets.AssetFile{
		{Path: "ssh/dec_github", Content: []byte("{{secret:ssh-dec-github}}")},
		{Path: "ssh/dec_github.pub", Content: []byte("ssh-ed25519 AAAA dec\n")},
		{Path: "ssh/config", Content: []byte("Include config.d/dec.conf\r\n")},
		{Path: "proj/app.env", Content: []byte("{{secret:proj-env}}")},
	}, `name: creds
items:
  - kind: file
    path: ssh/dec_github
    name: key
    planes: [user]
    root: ssh
    target: dec_github
  - kind: file
    path: ssh/dec_github.pub
    name: pub
    planes: [user]
    root: ssh
    target: dec_github.pub
    mode: 420
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
	f.registerDevice(t, dev("dev-1", "creds"))
	f.registerProject(t, ssot.Project{ID: "p1", DeviceID: "dev-1", Name: "proj", Path: "/tmp/proj", Bundles: []string{"creds"}})
}

func byTarget(out *Rendered) map[string]proto.File {
	m := map[string]proto.File{}
	for _, file := range out.Files {
		for _, tg := range file.File.Targets {
			m[tg.Root+"/"+tg.Path] = file.File
		}
	}
	return m
}

func TestRenderFileItems(t *testing.T) {
	f := newFixture(t)
	seedCredBundle(t, f)
	out, err := f.render.RenderFor(dev("dev-1", "creds"), credRoots)
	if err != nil {
		t.Fatalf("RenderFor: %v", err)
	}
	if len(out.Alerts) != 0 {
		t.Fatalf("不应有告警: %+v", out.Alerts)
	}
	got := byTarget(out)

	key, ok := got["ssh/dec_github"]
	if !ok {
		t.Fatalf("缺少私钥落点: %v", got)
	}
	if !key.Secret || key.Mode != 0o600 {
		t.Fatalf("私钥必须 Secret 且 0600: secret=%v mode=%#o", key.Secret, key.Mode)
	}
	if strings.Contains(key.Content, "\r") || !strings.HasSuffix(key.Content, "\n") {
		t.Fatalf("私钥应 LF 归一并以换行结尾: %q", key.Content)
	}
	if !strings.HasPrefix(key.Content, "-----BEGIN") {
		t.Fatalf("file 条目不应加说明行: %q", key.Content)
	}

	pub := got["ssh/dec_github.pub"]
	if pub.Secret || pub.Mode != 0o644 || pub.Content != "ssh-ed25519 AAAA dec\n" {
		t.Fatalf("公钥下发错误: %+v", pub)
	}
	if len(pub.Targets) != 1 || pub.Targets[0].Plan != proto.PlanUser {
		t.Fatalf("公钥只应落在用户平面: %+v", pub.Targets)
	}

	cfg := got["ssh/config"]
	if cfg.Format != proto.FormatManagedBlock || cfg.Content != "Include config.d/dec.conf\n" {
		t.Fatalf("config 托管块错误: format=%q content=%q", cfg.Format, cfg.Content)
	}

	env := got["secrets/app.env"]
	if !env.Secret || env.Mode != 0o600 || env.Content != "TOKEN=abc\n" {
		t.Fatalf("项目 secret 下发错误: %+v", env)
	}
	if len(env.Targets) != 1 || env.Targets[0].Plan != proto.PlanProject || env.Targets[0].ProjectID != "p1" {
		t.Fatalf("项目 secret 落点错误: %+v", env.Targets)
	}
}

func TestRenderFiltersUnsupportedRoots(t *testing.T) {
	f := newFixture(t)
	seedCredBundle(t, f)
	out, err := f.render.RenderFor(dev("dev-1", "creds"), []string{proto.RootSSH})
	if err != nil {
		t.Fatalf("RenderFor: %v", err)
	}
	if _, ok := byTarget(out)["secrets/app.env"]; ok {
		t.Fatal("代理没上报 secrets 根时不应下发项目 secret")
	}
	if out.TooOld() != 1 {
		t.Fatalf("应有 1 条版本过低告警，实际 %d: %+v", out.TooOld(), out.Alerts)
	}

	old, err := f.render.RenderFor(dev("dev-1", "creds"), nil)
	if err != nil {
		t.Fatalf("RenderFor: %v", err)
	}
	if len(old.Files) != 0 || old.TooOld() != 4 {
		t.Fatalf("老代理不应收到任何凭据条目: files=%d tooOld=%d", len(old.Files), old.TooOld())
	}
}

// TestSyncAdoptAndTooOldKeepsFiles 覆盖：首次下发标 Adopt、落地后不再标；
// 代理回退到不支持凭据根时，已落地的条目不被删除。
func TestSyncAdoptAndTooOldKeepsFiles(t *testing.T) {
	f := newFixture(t)
	seedCredBundle(t, f)
	d := dev("dev-1", "creds")

	first, err := f.render.Sync(d, proto.SyncRequest{Roots: credRoots})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if first.Batch == nil || len(first.Batch.Files) != 4 {
		t.Fatalf("首次应下发 4 条，实际 %+v", first.Batch)
	}
	var acks []proto.AckResult
	var manifest []proto.ManifestEntry
	for _, file := range first.Batch.Files {
		if !file.Adopt {
			t.Fatalf("首次下发的凭据条目应标 Adopt: %s", file.ID)
		}
		p := "/x/" + file.ID
		h := ssot.SumHex([]byte(file.Content))
		acks = append(acks, proto.AckResult{ID: file.ID, OK: true, Hash: h, Paths: []string{p}})
		manifest = append(manifest, proto.ManifestEntry{ID: file.ID, Hash: h, Paths: []string{p}, Format: file.Format, Key: file.Key})
	}
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, acks); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 本地被改：重新下发，但已有落地哈希，不再标 Adopt。
	changed := append([]proto.ManifestEntry(nil), manifest...)
	changed[0].Hash = ssot.SumHex([]byte("local"))
	again, err := f.render.Sync(d, proto.SyncRequest{Revision: first.Batch.Revision, Manifest: changed, Roots: credRoots})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if again.Batch == nil || len(again.Batch.Files) != 1 || again.Batch.Files[0].Adopt {
		t.Fatalf("已落地条目重发时不应标 Adopt: %+v", again.Batch)
	}

	// 代理回退：不上报凭据根，已落地的条目不能被删。
	back, err := f.render.Sync(d, proto.SyncRequest{Revision: first.Batch.Revision, Manifest: manifest})
	if err != nil {
		t.Fatalf("回退 Sync: %v", err)
	}
	if back.Batch != nil && len(back.Batch.Deletes) != 0 {
		t.Fatalf("老代理上不应删除凭据条目: %+v", back.Batch.Deletes)
	}
}

func TestRenderFileSlotConflict(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"a", "b"} {
		f.writeBundle(t, name, []assets.AssetFile{
			{Path: "pub", Content: []byte("ssh-ed25519 " + name + "\n")},
		}, "name: "+name+"\nitems:\n  - kind: file\n    path: pub\n    name: pub-"+name+"\n    planes: [user]\n    root: ssh\n    target: dec_x.pub\n")
	}
	f.registerDevice(t, dev("dev-1", "a", "b"))
	out, err := f.render.RenderFor(dev("dev-1", "a", "b"), credRoots)
	if err != nil {
		t.Fatalf("RenderFor: %v", err)
	}
	if len(out.Files) != 1 || len(out.Alerts) != 1 || !strings.Contains(out.Alerts[0].Reason, "落点冲突") {
		t.Fatalf("同落点应只下发一条并告警: files=%d alerts=%+v", len(out.Files), out.Alerts)
	}
}

func TestRecordRootsSkipsUnchanged(t *testing.T) {
	f := newFixture(t)
	s := f.render.States()
	if err := s.RecordRoots("dev-1", []string{"secrets", "ssh"}); err != nil {
		t.Fatalf("RecordRoots: %v", err)
	}
	path := filepath.Join(s.dir, "dev-1.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("状态文件应已写入: %v", err)
	}
	if err := os.Chtimes(path, before.ModTime().Add(-3600e9), before.ModTime().Add(-3600e9)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRoots("dev-1", []string{"ssh", "secrets"}); err != nil {
		t.Fatalf("RecordRoots: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime().Add(-3600e9)) {
		t.Fatal("落点根未变化时不应写盘")
	}
	roots, err := s.Roots("dev-1")
	if err != nil || len(roots) != 2 || roots[0] != "secrets" {
		t.Fatalf("Roots 返回错误: %v %v", roots, err)
	}
}
