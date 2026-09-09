package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	old := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = old })

	var out bytes.Buffer
	if err := run([]string{"--version"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "v1.2.3" {
		t.Fatalf("version = %q", got)
	}
}

func TestSetupIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEC_HOME", home)

	var first bytes.Buffer
	if err := run(nil, &first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "changed=true") ||
		!strings.Contains(first.String(), "host-setup=ok") {
		t.Fatalf("首次输出不完整: %s", first.String())
	}

	var second bytes.Buffer
	if err := run(nil, &second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "changed=false") {
		t.Fatalf("第二次应幂等: %s", second.String())
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "management_listen: 127.0.0.1:47653") {
		t.Fatalf("配置未写入约定地址:\n%s", data)
	}
}
