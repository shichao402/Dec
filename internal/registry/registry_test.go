package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTagRoundTrip(t *testing.T) {
	tag, err := Tag("relkit", "v0.3.24")
	if err != nil {
		t.Fatal(err)
	}
	if tag != "registry/relkit/v0.3.24" {
		t.Fatalf("tag = %q", tag)
	}
	p, v, err := ParseTag(tag)
	if err != nil {
		t.Fatal(err)
	}
	if p != "relkit" || v != "v0.3.24" {
		t.Fatalf("got %s %s", p, v)
	}
}

func TestYankedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	y := Yanked{}
	y.Add("relkit", "v0.3.20")
	if err := WriteYanked(dir, y); err != nil {
		t.Fatal(err)
	}
	got, err := LoadYanked(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Contains("relkit", "v0.3.20") {
		t.Fatalf("missing yank: %#v", got)
	}
	got.Remove("relkit", "v0.3.20")
	if got.Contains("relkit", "v0.3.20") {
		t.Fatal("still yanked")
	}
}

func TestLatestSkipsYanked(t *testing.T) {
	y := Yanked{"relkit": {"v0.3.25"}}
	got, err := LatestVersion("relkit", []string{"v0.3.24", "v0.3.25"}, y)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.3.24" {
		t.Fatalf("got %s", got)
	}
}

func TestLatestPrefersSemverOverLexicographic(t *testing.T) {
	// 字典序下 "v0.4.9" > "v0.4.10"；latest 必须按数值选 0.4.10。
	got, err := LatestVersion("relkit", []string{"v0.4.9", "v0.4.10", "v0.4.8"}, Yanked{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.4.10" {
		t.Fatalf("got %s, want v0.4.10", got)
	}
}

func TestCompareVersionSemver(t *testing.T) {
	if compareVersion("v0.4.10", "v0.4.9") <= 0 {
		t.Fatal("v0.4.10 should be newer than v0.4.9")
	}
	if compareVersion("v0.4.9", "v0.4.10") >= 0 {
		t.Fatal("v0.4.9 should be older than v0.4.10")
	}
	if compareVersion("v1.2.3", "v1.2.3") != 0 {
		t.Fatal("equal versions should compare equal")
	}
	if compareVersion("v1.10.0", "v1.9.99") <= 0 {
		t.Fatal("v1.10.0 should be newer than v1.9.99")
	}
}

func TestCompareVersionBuildRevision(t *testing.T) {
	// 字典序下 "+10" < "+9"；必须按修订号数值比较。
	if compareVersion("v0.1.0+10", "v0.1.0+9") <= 0 {
		t.Fatal("v0.1.0+10 should be newer than v0.1.0+9")
	}
	if compareVersion("v0.1.0+4", "v0.1.0+4") != 0 {
		t.Fatal("equal +N versions should compare equal")
	}
	if compareVersion("v0.1.0+4", "v0.1.0") <= 0 {
		t.Fatal("v0.1.0+4 should be newer than bare v0.1.0")
	}
	if compareVersion("v0.2.0", "v0.1.0+99") <= 0 {
		t.Fatal("higher semver should beat any +N on older base")
	}
}

func TestLatestPrefersNumericBuildRevision(t *testing.T) {
	got, err := LatestVersion("p", []string{"v0.1.0", "v0.1.0+2", "v0.1.0+9", "v0.1.0+10", "v0.1.0+4"}, Yanked{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.1.0+10" {
		t.Fatalf("got %s, want v0.1.0+10", got)
	}
}

func TestLatestAllYanked(t *testing.T) {
	y := Yanked{"relkit": {"v0.3.24"}}
	if _, err := LatestVersion("relkit", []string{"v0.3.24"}, y); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveExactMissing(t *testing.T) {
	if _, _, err := Resolve("relkit", "v9.9.9", []string{"v0.3.24"}, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteYankedEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadYanked(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, yankFile)); !os.IsNotExist(err) {
		t.Fatalf("stat: %v", err)
	}
}
