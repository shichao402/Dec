package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// 落盘层的两条硬规则（需求 2.7、6.3、6.4）：
// 含密钥的文件 0600；共享配置文件只改 dec- 前缀的键，且哈希只算托管条目。

func applyOne(t *testing.T, home string, f proto.File) *WriteResult {
	t.Helper()
	w := NewWriter(NewResolver(home, nil), home)
	res, err := w.Apply(f)
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	return res
}

func TestApplyWholeFileAndNotice(t *testing.T) {
	home := testHome(t)
	res := applyOne(t, home, proto.File{
		ID:      "asset:woa:style",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
		Content: "# style\n",
		Format:  proto.FormatWhole,
	})
	path := filepath.Join(home, ".cursor", "rules", "style.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取落点失败: %v", err)
	}
	if !strings.Contains(string(data), managedNotice) {
		t.Fatalf("整文件落点应带管理说明，实际内容：%q", data)
	}
	if !strings.Contains(string(data), "# style") {
		t.Fatalf("原始内容丢失：%q", data)
	}
	if res.Hash == "" {
		t.Fatal("回执哈希不应为空")
	}
	// 回执哈希必须与现场算出来的一致，否则服务端会一直重发。
	again, err := HashFile(path, proto.FormatWhole, "")
	if err != nil {
		t.Fatalf("计算哈希失败: %v", err)
	}
	if again != res.Hash {
		t.Fatalf("回执哈希 %q 与现场哈希 %q 不一致", res.Hash, again)
	}
}

func TestApplySecretFileMode0600(t *testing.T) {
	home := testHome(t)
	applyOne(t, home, proto.File{
		ID:      "secret:token",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: RootDec, Path: "env/secrets.env"}},
		Content: "TOKEN=abc\n",
		Secret:  true,
		Format:  proto.FormatWhole,
	})
	path := filepath.Join(home, ".dec", "env", "secrets.env")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取落点失败: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("含密钥文件权限 %o，期望 600", got)
	}
	// 含密钥的文件不写管理说明：多一行注释可能被解析器当成变量。
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), managedNotice) {
		t.Fatalf("含密钥文件不应写入说明：%q", data)
	}
}

func TestApplyJSONKeyKeepsOthers(t *testing.T) {
	home := testHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, FileMCP)
	user := `{"mcpServers":{"mine":{"command":"x","args":["a"]}},  "zeta":  1}`
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatalf("写入前置文件失败: %v", err)
	}

	applyOne(t, home, proto.File{
		ID:      "asset:woa:demo",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: FileMCP}},
		Content: `{"command":"dec","args":["serve"]}`,
		Format:  proto.FormatJSONKey,
		Key:     "dec-demo",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("结果不是合法 JSON: %v\n%s", err, data)
	}
	if _, ok := root["dec-demo"]; !ok {
		t.Fatalf("托管条目未写入：%s", data)
	}
	// 用户自己的键必须原样保留。
	var mine map[string]any
	if err := json.Unmarshal(root["mcpServers"], &mine); err != nil {
		t.Fatalf("用户条目结构被破坏: %v", err)
	}
	if _, ok := mine["mine"]; !ok {
		t.Fatalf("用户条目丢失：%s", data)
	}
}

func TestApplyJSONKeyRejectsBadPrefix(t *testing.T) {
	home := testHome(t)
	w := NewWriter(NewResolver(home, nil), home)
	_, err := w.Apply(proto.File{
		ID:      "asset:woa:demo",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: FileMCP}},
		Content: `{"command":"dec"}`,
		Format:  proto.FormatJSONKey,
		Key:     "demo",
	})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("非 dec- 前缀的键应被拒绝，实际 err=%v", err)
	}
}

func TestApplyJSONKeyRefusesBrokenJSON(t *testing.T) {
	home := testHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, FileMCP)
	// 用户手写坏了共享配置：宁可报错，也不能静默把他的配置清空。
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("写入前置文件失败: %v", err)
	}
	w := NewWriter(NewResolver(home, nil), home)
	_, err := w.Apply(proto.File{
		ID:      "asset:woa:demo",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: FileMCP}},
		Content: `{"command":"dec"}`,
		Format:  proto.FormatJSONKey,
		Key:     "dec-demo",
	})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("非法 JSON 应被拒绝，实际 err=%v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{not json" {
		t.Fatalf("失败时不应改动文件，实际：%q", data)
	}
}

func TestHashIgnoresUserKeys(t *testing.T) {
	home := testHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, FileMCP)
	base := `{"dec-a":{"command":"x"},"mine":{"command":"y"}}`
	if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	before, err := HashFile(path, proto.FormatJSONKey, "dec-a")
	if err != nil {
		t.Fatalf("计算哈希失败: %v", err)
	}

	// 用户新增一个自己的 MCP：不应影响托管条目的哈希。
	changed := `{"dec-a":{"command":"x"},"mine":{"command":"y"},"other":{"command":"z"}}`
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	after, err := HashFile(path, proto.FormatJSONKey, "dec-a")
	if err != nil {
		t.Fatalf("计算哈希失败: %v", err)
	}
	if before != after {
		t.Fatalf("用户键变化不应改变托管哈希：%q -> %q", before, after)
	}

	// 托管条目自己变了，必须被发现。
	broken := `{"dec-a":{"command":"CHANGED"},"mine":{"command":"y"}}`
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	tampered, err := HashFile(path, proto.FormatJSONKey, "dec-a")
	if err != nil {
		t.Fatalf("计算哈希失败: %v", err)
	}
	if tampered == before {
		t.Fatalf("托管条目被改应改变哈希，实际仍为 %q", before)
	}
}

func TestHashMissingFileIsEmpty(t *testing.T) {
	home := testHome(t)
	got, err := HashFile(filepath.Join(home, "nope.md"), proto.FormatWhole, "")
	if err != nil {
		t.Fatalf("缺少文件不应报错: %v", err)
	}
	if got != "" {
		t.Fatalf("缺少文件应返回空哈希，实际 %q", got)
	}
}

func TestRemoveWholeAndCleanUpDirs(t *testing.T) {
	home := testHome(t)
	f := proto.File{
		ID:      "asset:woa:style",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
		Content: "# style\n",
		Format:  proto.FormatWhole,
	}
	applyOne(t, home, f)
	w := NewWriter(NewResolver(home, nil), home)
	if _, err := w.Remove(f); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "rules", "style.md")); !os.IsNotExist(err) {
		t.Fatalf("文件应已删除，err=%v", err)
	}
	// 空的类别目录顺带删掉，但不该继续往上删 .cursor。
	if _, err := os.Stat(filepath.Join(home, ".cursor", "rules")); !os.IsNotExist(err) {
		t.Fatalf("空的 rules 目录应被删除，err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor")); err != nil {
		t.Fatalf("宿主目录不应被删除: %v", err)
	}
}

func TestRemoveJSONKeyKeepsUserContent(t *testing.T) {
	home := testHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, FileMCP)
	content := `{"dec-a":{"command":"x"},"mine":{"command":"y"}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := removeJSONKey(path, "dec-a"); err != nil {
		t.Fatalf("删除键失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("文件不应被删除: %v", err)
	}
	if strings.Contains(string(data), "dec-a") {
		t.Fatalf("托管键未删除：%s", data)
	}
	if !strings.Contains(string(data), "mine") {
		t.Fatalf("用户键被误删：%s", data)
	}
}

func TestRemoveJSONKeyDropsEmptyFile(t *testing.T) {
	home := testHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, FileMCP)
	if err := os.WriteFile(path, []byte(`{"dec-a":{"command":"x"}}`), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := removeJSONKey(path, "dec-a"); err != nil {
		t.Fatalf("删除键失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("只剩空容器时应删掉文件，err=%v", err)
	}
}
