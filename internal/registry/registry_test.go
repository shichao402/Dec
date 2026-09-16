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
