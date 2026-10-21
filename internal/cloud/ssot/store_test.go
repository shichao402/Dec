package ssot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOpenCreatesInitialCommit(t *testing.T) {
	s := newTestStore(t)
	if s.Head() == "" {
		t.Fatal("初始化后应有 revision")
	}
	if _, err := s.ReadFile(FileDevices); err != nil {
		t.Fatalf("设备表应已创建: %v", err)
	}
	table, err := ReadJSON[DeviceTable](s, FileDevices)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if table.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version 错误: %d", table.SchemaVersion)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	head1 := s1.Head()

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("再次 Open: %v", err)
	}
	if s2.Head() != head1 {
		t.Fatalf("重复 Open 产生了新提交: %s -> %s", head1, s2.Head())
	}
}

func TestUpdateCommitsAndAdvancesRevision(t *testing.T) {
	s := newTestStore(t)
	before := s.Head()

	rev, err := s.Update(Author{Name: "dev-1", Source: "mcp"}, func(tx *Tx) (string, error) {
		return "写入一个资产", tx.WriteFile("assets/demo/skill/a.md", []byte("# hi"), 0o600)
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rev == before {
		t.Fatal("revision 未推进")
	}
	if s.Head() != rev {
		t.Fatalf("Head 未更新: %s != %s", s.Head(), rev)
	}

	data, err := s.ReadFile("assets/demo/skill/a.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "# hi" {
		t.Fatalf("内容错误: %q", data)
	}

	hist, err := s.History(context.Background(), 3)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("历史条数应为 2，实际 %d", len(hist))
	}
	if hist[0].Message != "写入一个资产" {
		t.Fatalf("最新提交 message 错误: %q", hist[0].Message)
	}
	if hist[0].Source != "mcp" {
		t.Fatalf("来源未写入 trailer: %q", hist[0].Source)
	}
	if hist[0].Author != "dev-1" {
		t.Fatalf("作者错误: %q", hist[0].Author)
	}
}

func TestUpdateWithoutChangesKeepsRevision(t *testing.T) {
	s := newTestStore(t)
	before := s.Head()
	rev, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		return "空操作", nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rev != before {
		t.Fatalf("空操作不应推进 revision: %s != %s", rev, before)
	}
}

func TestUpdateRollsBackOnError(t *testing.T) {
	s := newTestStore(t)
	before := s.Head()

	_, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		if err := tx.WriteFile("assets/demo/f.md", []byte("data"), 0o600); err != nil {
			return "", err
		}
		return "", errors.New("校验失败")
	})
	if err == nil {
		t.Fatal("期望错误")
	}
	if s.Head() != before {
		t.Fatal("失败回调不应推进 revision")
	}
	if _, err := s.ReadFile("assets/demo/f.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("失败回调写入的文件应被回滚，实际 %v", err)
	}
}

func TestUpdateRestoresOverwrittenFileOnError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		return "初始内容", tx.WriteFile("assets/demo/f.md", []byte("v1"), 0o600)
	}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	_, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		if err := tx.WriteFile("assets/demo/f.md", []byte("v2"), 0o600); err != nil {
			return "", err
		}
		return "", errors.New("boom")
	})
	if err == nil {
		t.Fatal("期望错误")
	}
	data, err := s.ReadFile("assets/demo/f.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "v1" {
		t.Fatalf("应恢复到 v1，实际 %q", data)
	}
}

func TestEnsureRevision(t *testing.T) {
	s := newTestStore(t)
	base := s.Head()

	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		if err := tx.EnsureRevision(""); err != nil {
			return "", err
		}
		return "第一次写入", tx.WriteFile("assets/demo/a.md", []byte("a"), 0o600)
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	_, err := s.Update(Author{Name: "y"}, func(tx *Tx) (string, error) {
		if err := tx.EnsureRevision(base); err != nil {
			return "", err
		}
		return "过期写入", tx.WriteFile("assets/demo/b.md", []byte("b"), 0o600)
	})
	if !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("期望 ErrRevisionMismatch，实际 %v", err)
	}

	current := s.Head()
	if _, err := s.Update(Author{Name: "y"}, func(tx *Tx) (string, error) {
		if err := tx.EnsureRevision(current); err != nil {
			return "", err
		}
		return "基于最新写入", tx.WriteFile("assets/demo/b.md", []byte("b"), 0o600)
	}); err != nil {
		t.Fatalf("基于最新 revision 的写入应成功: %v", err)
	}
}

func TestDeleteFileAndDir(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		if err := tx.WriteFile("assets/demo/skill/a.md", []byte("a"), 0o600); err != nil {
			return "", err
		}
		if err := tx.WriteFile("assets/demo/skill/sub/b.md", []byte("b"), 0o600); err != nil {
			return "", err
		}
		return "建目录", nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 幂等：删除不存在的文件不报错。
	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		return "删除不存在的文件", tx.DeleteFile("assets/demo/none.md")
	}); err != nil {
		t.Fatalf("删除不存在文件应幂等: %v", err)
	}

	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		return "删除 skill 目录", tx.DeleteDir("assets/demo/skill")
	}); err != nil {
		t.Fatalf("DeleteDir: %v", err)
	}
	if _, err := s.ReadFile("assets/demo/skill/a.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("目录应已删除，实际 %v", err)
	}
	// 目录确实从磁盘消失。
	if _, err := os.Stat(filepath.Join(s.Dir(), "assets/demo/skill")); !os.IsNotExist(err) {
		t.Fatalf("目录仍存在: %v", err)
	}
}

func TestSafeRelPathRejectedInTx(t *testing.T) {
	s := newTestStore(t)
	bad := []string{"../escape", "/abs/path", "a//b", "", "assets/../../x"}
	for _, p := range bad {
		_, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
			return "坏路径", tx.WriteFile(p, []byte("x"), 0o600)
		})
		if err == nil {
			t.Fatalf("路径 %q 应被拒绝", p)
		}
	}
}

func TestFileAtAndHistory(t *testing.T) {
	s := newTestStore(t)
	firstRev, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		return "v1", tx.WriteFile("secrets/demo.json", []byte(`{"v":1}`), 0o600)
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		return "v2", tx.WriteFile("secrets/demo.json", []byte(`{"v":2}`), 0o600)
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	data, err := s.FileAt(firstRev, "secrets/demo.json")
	if err != nil {
		t.Fatalf("FileAt: %v", err)
	}
	if string(data) != `{"v":1}` {
		t.Fatalf("历史内容错误: %s", data)
	}

	if _, err := s.FileAt(firstRev, "secrets/absent.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
	if _, err := s.FileAt(strings.Repeat("0", 40), "secrets/demo.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

func TestListDirAndWalkFiles(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
		if err := tx.WriteFile("assets/b1/manifest.yaml", []byte("name: b1\n"), 0o600); err != nil {
			return "", err
		}
		if err := tx.WriteFile("assets/b2/manifest.yaml", []byte("name: b2\n"), 0o600); err != nil {
			return "", err
		}
		return "两个 bundle", nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	bundles, err := s.ListDir(DirAssets)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(bundles) != 2 || bundles[0] != "b1" || bundles[1] != "b2" {
		t.Fatalf("bundle 列表错误: %v", bundles)
	}

	files, err := s.WalkFiles("assets/b1")
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "assets/b1/manifest.yaml" {
		t.Fatalf("文件列表错误: %v", files)
	}
}

func TestConcurrentUpdatesSerialize(t *testing.T) {
	s := newTestStore(t)
	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := s.Update(Author{Name: "x"}, func(tx *Tx) (string, error) {
				return "并发写入", tx.WriteFile("assets/b/f"+string(rune('a'+i))+".md", []byte("x"), 0o600)
			})
			errs <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("并发写入失败: %v", err)
		}
	}
	files, err := s.WalkFiles("assets/b")
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	if len(files) != n {
		t.Fatalf("应有 %d 个文件，实际 %d", n, len(files))
	}
	hist, err := s.History(context.Background(), 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != n+1 {
		t.Fatalf("提交数应为 %d，实际 %d", n+1, len(hist))
	}
}

func TestHalting(t *testing.T) {
	if err := ValidateBundle("_cloud"); err == nil {
		t.Fatal("_cloud 应保留")
	}
	if err := ValidateBundle("ok-bundle_1"); err != nil {
		t.Fatalf("合法 bundle 名被拒: %v", err)
	}
	if err := ValidateItemName("Bad Name"); err == nil {
		t.Fatal("含空格的条目名应被拒绝")
	}
	if EntryKey("x") != "dec-x" {
		t.Fatal("EntryKey 前缀错误")
	}
	if !HasEntryPrefix("dec-x") || HasEntryPrefix("mcpServers") {
		t.Fatal("HasEntryPrefix 判定错误")
	}
	if SumHex([]byte("a")) == SumHex([]byte("b")) {
		t.Fatal("哈希碰撞")
	}
	if !strings.HasPrefix(SumHex([]byte("a")), Prefix) {
		t.Fatal("哈希前缀错误")
	}
}
