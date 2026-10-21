package prune

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// mustExpiredAt 返回在 at 时刻已到期的条目数量，用于交叉校验 Expiring 与 Purge 的口径。
func mustExpiredAt(t *testing.T, store *Store, at time.Time) []Entry {
	t.Helper()
	entries, err := store.scan()
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Expired(at) {
			out = append(out, e)
		}
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

func TestMoveRemovesOriginalAndKeepsCopy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "project", ".secrets", "demo", "env", "app.env")
	writeFile(t, src, "TOKEN=abc\n")

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	store := New(filepath.Join(root, ".pruned"), Options{Now: fixedClock(now)})

	entry, err := store.Move(src, Entry{
		OriginalPath: src,
		DisplayPath:  ".secrets/demo/env/app.env",
		RelPath:      "env/app.env",
		Kind:         "note",
		Reason:       ReasonRemoteGone,
		Bundle:       "demo",
		Plane:        "project",
	})
	if err != nil {
		t.Fatalf("移入隔离区失败: %v", err)
	}

	if _, statErr := os.Lstat(src); !os.IsNotExist(statErr) {
		t.Fatalf("原件应已不在原位, statErr=%v", statErr)
	}
	stored := filepath.Join(store.ItemDir(entry.ID), entry.StoredRel)
	data, readErr := os.ReadFile(stored)
	if readErr != nil {
		t.Fatalf("隔离副本不可读: %v", readErr)
	}
	if string(data) != "TOKEN=abc\n" {
		t.Fatalf("隔离副本内容不符: %q", string(data))
	}
	if !entry.ExpiresAt.Equal(now.Add(DefaultRetention)) {
		t.Fatalf("保留期不符: %v", entry.ExpiresAt)
	}
	if entry.ID == "" {
		t.Fatal("隔离项应有稳定 ID")
	}
}

func TestMoveDirectoryKeepsTree(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "project", ".secrets", "demo")
	writeFile(t, filepath.Join(src, "env", "a.env"), "A=1\n")
	writeFile(t, filepath.Join(src, "files", "k.pem"), "KEY\n")

	store := New(filepath.Join(root, ".pruned"), Options{})
	entry, err := store.Move(src, Entry{OriginalPath: src, DisplayPath: ".secrets/demo", Kind: "dir", Reason: ReasonDerivedBundle})
	if err != nil {
		t.Fatalf("移入目录失败: %v", err)
	}
	if !entry.IsDir {
		t.Fatal("应标记为目录")
	}
	if entry.SizeBytes <= 0 {
		t.Fatalf("目录大小应大于 0, got %d", entry.SizeBytes)
	}
	stored := filepath.Join(store.ItemDir(entry.ID), entry.StoredRel)
	if _, statErr := os.Stat(filepath.Join(stored, "env", "a.env")); statErr != nil {
		t.Fatalf("目录树应完整保留: %v", statErr)
	}
}

func TestSameOriginalPathTwiceDoesNotCollide(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "project", ".secrets", "demo", "app.env")
	store := New(filepath.Join(root, ".pruned"), Options{})

	writeFile(t, src, "v1\n")
	first, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
	if err != nil {
		t.Fatalf("第一次移入失败: %v", err)
	}

	writeFile(t, src, "v2\n")
	second, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
	if err != nil {
		t.Fatalf("第二次移入失败: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("两次隔离不应共用同一目录: %s", first.ID)
	}

	entries, err := store.List(false)
	if err != nil {
		t.Fatalf("列举失败: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("应有两条隔离记录, got %d", len(entries))
	}
	// 内容互不覆盖。
	v1, _ := os.ReadFile(filepath.Join(store.ItemDir(first.ID), first.StoredRel))
	v2, _ := os.ReadFile(filepath.Join(store.ItemDir(second.ID), second.StoredRel))
	got := []string{string(v1), string(v2)}
	sort.Strings(got)
	if got[0] != "v1\n" || got[1] != "v2\n" {
		t.Fatalf("两次隔离内容应各自保留, got %q", got)
	}
}

func TestRestoreRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "project", ".secrets", "demo", "app.env")
	writeFile(t, src, "old\n")
	store := New(filepath.Join(root, ".pruned"), Options{})

	entry, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
	if err != nil {
		t.Fatalf("移入失败: %v", err)
	}

	writeFile(t, src, "new\n")
	if _, restoreErr := store.Restore(entry.ID); !errors.Is(restoreErr, ErrConflict) {
		t.Fatalf("目标已存在应返回 ErrConflict, got %v", restoreErr)
	}
	data, _ := os.ReadFile(src)
	if string(data) != "new\n" {
		t.Fatalf("冲突时不得覆盖现有文件, got %q", string(data))
	}

	if err := os.Remove(src); err != nil {
		t.Fatalf("清理目标失败: %v", err)
	}
	if _, err := store.Restore(entry.ID); err != nil {
		t.Fatalf("目标释放后应可恢复: %v", err)
	}
	restored, _ := os.ReadFile(src)
	if string(restored) != "old\n" {
		t.Fatalf("恢复内容不符: %q", string(restored))
	}
}

func TestRestoreAllReportsEachError(t *testing.T) {
	root := t.TempDir()
	store := New(filepath.Join(root, ".pruned"), Options{})

	var ids []string
	conflictPath := ""
	for i, name := range []string{"a.env", "b.env", "c.env"} {
		src := filepath.Join(root, "project", name)
		writeFile(t, src, name)
		entry, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
		if err != nil {
			t.Fatalf("移入 %s 失败: %v", name, err)
		}
		ids = append(ids, entry.ID)
		if i == 1 {
			conflictPath = src
		}
	}

	writeFile(t, conflictPath, "occupied\n")
	restored, errs := store.RestoreAll(ids)
	if len(restored) != 2 {
		t.Fatalf("应恢复两条, got %d", len(restored))
	}
	if len(errs) != 1 || !errors.Is(errs[0], ErrConflict) {
		t.Fatalf("应报告一条冲突, got %v", errs)
	}
}

func TestExpiredEntryCannotBeRestored(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "project", "a.env")
	writeFile(t, src, "v\n")

	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	store := New(filepath.Join(root, ".pruned"), Options{Now: func() time.Time { return clock }, Retention: time.Hour})

	entry, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
	if err != nil {
		t.Fatalf("移入失败: %v", err)
	}
	clock = base.Add(2 * time.Hour)
	if _, err := store.Restore(entry.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("过期后应拒绝恢复, got %v", err)
	}
}

func TestListHidesExpiredWhenAsked(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	store := New(filepath.Join(root, ".pruned"), Options{Now: fixedClock(base), Retention: time.Hour})
	src := filepath.Join(root, "project", "a.env")
	writeFile(t, src, "v\n")
	if _, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone}); err != nil {
		t.Fatalf("移入失败: %v", err)
	}

	later := New(filepath.Join(root, ".pruned"), Options{Now: fixedClock(base.Add(2 * time.Hour)), Retention: time.Hour})
	visible, err := later.List(false)
	if err != nil {
		t.Fatalf("列举失败: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("过期项不应出现在默认列表, got %d", len(visible))
	}
	all, err := later.List(true)
	if err != nil {
		t.Fatalf("列举失败: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("包含过期项时应返回 1 条, got %d", len(all))
	}
}

func TestPurgeExpiredAndExpiringPreview(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	store := New(filepath.Join(root, ".pruned"), Options{Now: fixedClock(base), Retention: time.Hour})

	src := filepath.Join(root, "project", "a.env")
	writeFile(t, src, "v\n")
	entry, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
	if err != nil {
		t.Fatalf("移入失败: %v", err)
	}

	// 到期时间是 base+1h：提前 30 分钟看，能看到将被清理的项。
	preview, err := store.Expiring(base.Add(90 * time.Minute))
	if err != nil {
		t.Fatalf("到期预览失败: %v", err)
	}
	if len(preview) != 1 || preview[0].ID != entry.ID {
		t.Fatalf("到期预览应包含该条目, got %#v", preview)
	}
	// 早在 base+30m 时它还没到期，不应出现在「届时会被清理」的清单里。
	early, err := store.Expiring(base.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("到期预览失败: %v", err)
	}
	if len(early) != 0 {
		t.Fatalf("未到期时预览应为空, got %d", len(early))
	}
	// 当前时刻的预览即「已经到期的项」。
	if len(preview) != len(mustExpiredAt(t, store, base.Add(2*time.Hour))) {
		t.Fatalf("到期清单应与 PurgeExpired 的判断一致")
	}

	late := New(filepath.Join(root, ".pruned"), Options{Now: fixedClock(base.Add(2 * time.Hour)), Retention: time.Hour})
	purged, err := late.PurgeExpired()
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if len(purged) != 1 {
		t.Fatalf("应清理一条, got %d", len(purged))
	}
	if _, statErr := os.Stat(late.ItemDir(entry.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("清理后目录应消失, statErr=%v", statErr)
	}
}

func TestEnforceCapacityDropsOldestFirst(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	store := New(filepath.Join(root, ".pruned"), Options{Now: func() time.Time { return clock }, MaxBytes: 60})

	var ids []string
	for i, name := range []string{"a", "b", "c"} {
		src := filepath.Join(root, "project", name)
		writeFile(t, src, "0123456789012345678901234567890123456789") // 40B
		entry, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
		if err != nil {
			t.Fatalf("移入 %s 失败: %v", name, err)
		}
		ids = append(ids, entry.ID)
		clock = base.Add(time.Duration(i+1) * time.Minute)
	}

	purged, err := store.EnforceCapacity()
	if err != nil {
		t.Fatalf("容量清理失败: %v", err)
	}
	if len(purged) != 2 {
		t.Fatalf("120B 超 60B 上限应清理两条, got %d", len(purged))
	}
	if purged[0].ID != ids[0] || purged[1].ID != ids[1] {
		t.Fatalf("应按最早移入优先清理, got %v", []string{purged[0].ID, purged[1].ID})
	}
	remaining, err := store.List(true)
	if err != nil {
		t.Fatalf("列举失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != ids[2] {
		t.Fatalf("应只保留最新一条, got %#v", remaining)
	}
}

func TestCorruptManifestSkipsOnlyThatEntry(t *testing.T) {
	root := t.TempDir()
	store := New(filepath.Join(root, ".pruned"), Options{})

	goodSrc := filepath.Join(root, "project", "good.env")
	writeFile(t, goodSrc, "good\n")
	good, err := store.Move(goodSrc, Entry{OriginalPath: goodSrc, Reason: ReasonRemoteGone})
	if err != nil {
		t.Fatalf("移入失败: %v", err)
	}

	badDir := filepath.Join(store.Root(), "20260101T000000.000000000Z-deadbeef00")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatalf("创建坏目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, manifestFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("写入坏清单失败: %v", err)
	}

	entries, err := store.List(true)
	if err != nil {
		t.Fatalf("坏清单不应导致整次列举失败: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != good.ID {
		t.Fatalf("应只返回完好条目, got %#v", entries)
	}

	if _, err := store.Restore("20260101T000000.000000000Z-deadbeef00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("坏清单项应视为不存在, got %v", err)
	}
}

func TestUnwritableRootReportsErrUnwritable(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("写入占位文件失败: %v", err)
	}
	// 隔离根被一个普通文件占住，MkdirAll 必然失败。
	store := New(filepath.Join(blocker, "nested"), Options{})

	src := filepath.Join(root, "project", "a.env")
	writeFile(t, src, "v\n")
	_, err := store.Move(src, Entry{OriginalPath: src, Reason: ReasonRemoteGone})
	if !errors.Is(err, ErrUnwritable) {
		t.Fatalf("应返回 ErrUnwritable, got %v", err)
	}
	if _, statErr := os.Lstat(src); statErr != nil {
		t.Fatalf("失败时原件必须保持不动, statErr=%v", statErr)
	}
}

func TestReadmeDefaultsAreUsable(t *testing.T) {
	if DefaultRetention <= 0 {
		t.Fatal("默认保留期应为正值")
	}
	if DefaultMaxBytes <= 0 {
		t.Fatal("默认容量上限应为正值")
	}
	store := New("/tmp/unused", Options{})
	if store.Retention() != DefaultRetention {
		t.Fatalf("零值应取默认保留期, got %v", store.Retention())
	}
	if store.MaxBytes() != DefaultMaxBytes {
		t.Fatalf("零值应取默认容量, got %d", store.MaxBytes())
	}
}
