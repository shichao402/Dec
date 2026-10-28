package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/agent/legacy"
	"github.com/shichao402/Dec/internal/proto"
)

func archiveCount(t *testing.T, state string) int {
	t.Helper()
	entries, err := os.ReadDir(legacy.ArchiveDir(state))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			n++
		}
	}
	return n
}

func writeExisting(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptSameContentDoesNotArchive(t *testing.T) {
	home, _, w := credEnv(t)
	state := t.TempDir()
	w.SetArchiveDir(state)
	p := filepath.Join(home, ".ssh", "dec_a.pub")
	writeExisting(t, p, "ssh-ed25519 AAAA\n")
	res, err := w.Apply(proto.File{
		ID: "pub", Targets: []proto.Target{sshTarget("dec_a.pub")},
		Content: "ssh-ed25519 AAAA\n", Format: proto.FormatWhole, Adopt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 || archiveCount(t, state) != 0 {
		t.Fatalf("内容一致时不应归档: warn=%v", res.Warnings)
	}
}

func TestAdoptDifferentContentArchivesThenOverwrites(t *testing.T) {
	home, project, w := credEnv(t)
	state := t.TempDir()
	w.SetArchiveDir(state)

	key := filepath.Join(home, ".ssh", "dec_a")
	writeExisting(t, key, "old-key\n")
	res, err := w.Apply(proto.File{
		ID: "key", Targets: []proto.Target{sshTarget("dec_a")},
		Content: "new-key\n", Secret: true, Format: proto.FormatWhole, Adopt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(key); string(data) != "new-key\n" {
		t.Fatalf("应覆盖为新内容: %q", data)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "已归档后覆盖") {
		t.Fatalf("回执应说明已归档: %v", res.Warnings)
	}

	env := filepath.Join(project, ".secrets", "app.env")
	writeExisting(t, env, "OLD=1\n")
	res, err = w.Apply(proto.File{
		ID: "env", Targets: []proto.Target{secretTarget("app.env")},
		Content: "NEW=1\n", Secret: true, Format: proto.FormatWhole, Adopt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(res.Warnings, "已归档后覆盖") {
		t.Fatalf("项目 secret 回执应说明已归档: %v", res.Warnings)
	}
	if archiveCount(t, state) != 2 {
		t.Fatalf("应有两份归档，实际 %d", archiveCount(t, state))
	}
}

func TestAdoptWithoutAdoptFlagDoesNotArchive(t *testing.T) {
	home, _, w := credEnv(t)
	state := t.TempDir()
	w.SetArchiveDir(state)
	key := filepath.Join(home, ".ssh", "dec_a")
	writeExisting(t, key, "old\n")
	if _, err := w.Apply(proto.File{
		ID: "key", Targets: []proto.Target{sshTarget("dec_a")}, Content: "new\n", Format: proto.FormatWhole,
	}); err != nil {
		t.Fatal(err)
	}
	if archiveCount(t, state) != 0 {
		t.Fatal("非首次接管不应归档")
	}
}

func TestAdoptRefusesWithoutStateDir(t *testing.T) {
	home, _, w := credEnv(t)
	key := filepath.Join(home, ".ssh", "dec_a")
	writeExisting(t, key, "old\n")
	_, err := w.Apply(proto.File{
		ID: "key", Targets: []proto.Target{sshTarget("dec_a")}, Content: "new\n", Format: proto.FormatWhole, Adopt: true,
	})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("没有归档目录时应拒绝覆盖: %v", err)
	}
	if data, _ := os.ReadFile(key); string(data) != "old\n" {
		t.Fatalf("拒绝时原文件不应被改: %q", data)
	}
}

func TestAdoptSSHConfig(t *testing.T) {
	home, _, w := credEnv(t)
	state := t.TempDir()
	w.SetArchiveDir(state)
	cfg := filepath.Join(home, ".ssh", "config")
	writeExisting(t, cfg, "Host a\n  User x\n")

	// 没有托管块：只插块，不归档。
	res, err := w.Apply(proto.File{
		ID: "cfg", Targets: []proto.Target{sshTarget("config")},
		Content: "Include config.d/dec.conf\n", Format: proto.FormatManagedBlock, Adopt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 || archiveCount(t, state) != 0 {
		t.Fatalf("无托管块时不应归档: %v", res.Warnings)
	}

	// 已有不同的托管块（例如旧系统写的）：先归档再替换。
	res, err = w.Apply(proto.File{
		ID: "cfg", Targets: []proto.Target{sshTarget("config")},
		Content: "Include config.d/dec2.conf\n", Format: proto.FormatManagedBlock, Adopt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(res.Warnings, "已归档后覆盖") || archiveCount(t, state) != 1 {
		t.Fatalf("托管块不同时应归档: %v", res.Warnings)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), "Host a\n  User x\n") || !strings.Contains(string(data), "dec2.conf") {
		t.Fatalf("用户原有内容应保留、托管块应替换: %q", data)
	}
}

func hasNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}
