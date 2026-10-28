package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

func TestLocalCredentialStatus(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(filepath.Join(ssh, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	put := func(p, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(ssh, "dec_ok"), "k\n", 0o600)
	put(filepath.Join(ssh, "dec_bad"), "k\r\n", 0o644)
	put(filepath.Join(ssh, "id_rsa"), "not mine\n", 0o600)
	put(filepath.Join(ssh, "config.d", "dec.conf"), "Host x\n", 0o644)
	put(filepath.Join(ssh, "config"), "Host a\n"+proto.BlockBegin+"\nInclude x\n"+proto.BlockEnd+"\n", 0o600)

	project := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(project, ".secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	put(filepath.Join(project, ".secrets", "app.env"), "SECRET_VALUE\n", 0o600)

	slots, err := LocalCredentialStatus(home, []Project{{ID: "p1", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]LocalSlot{}
	for _, s := range slots {
		byPath[s.Path] = s
		for _, is := range s.Issues {
			if strings.Contains(is, "SECRET_VALUE") {
				t.Fatal("状态不应输出内容")
			}
		}
	}
	if _, ok := byPath[filepath.Join(ssh, "id_rsa")]; ok {
		t.Fatal("白名单外的文件不应列出")
	}
	if s := byPath[filepath.Join(ssh, "dec_ok")]; len(s.Issues) != 0 {
		t.Fatalf("正常私钥不应有问题: %v", s.Issues)
	}
	bad := byPath[filepath.Join(ssh, "dec_bad")]
	if !hasNote(bad.Issues, "CRLF") || (runtime.GOOS != "windows" && !hasNote(bad.Issues, "权限")) {
		t.Fatalf("应报出权限与行尾问题: %v", bad.Issues)
	}
	if _, ok := byPath[filepath.Join(ssh, "config.d", "dec.conf")]; !ok {
		t.Fatal("应列出 config.d/dec.conf")
	}
	if _, ok := byPath[filepath.Join(ssh, "config")+"（托管块）"]; !ok {
		t.Fatal("应列出 config 托管块")
	}
	if _, ok := byPath[filepath.Join(project, ".secrets", ".gitignore")]; !ok {
		t.Fatal("缺少 .gitignore 应报出")
	}
	if _, ok := byPath[filepath.Join(project, ".secrets", "app.env")]; !ok {
		t.Fatal("应列出项目 secret")
	}
}
