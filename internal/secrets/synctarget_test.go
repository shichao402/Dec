package secrets

import "testing"

func TestDefaultBundleFolder(t *testing.T) {
	if got := DefaultBundleFolder("vikunja"); got != "bundle/vikunja" {
		t.Fatalf("DefaultBundleFolder(vikunja) = %q", got)
	}
	if got := DefaultBundleFolder("bundle/vikunja"); got != "bundle/vikunja" {
		t.Fatalf("已带前缀不应重复: %q", got)
	}
	target, err := NewBundleSyncTarget("tencent-cloud", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Folder != "bundle/tencent-cloud" {
		t.Fatalf("Folder = %q", target.Folder)
	}
	target2, err := NewBundleSyncTarget("tencent-cloud", "custom")
	if err != nil {
		t.Fatal(err)
	}
	if target2.Folder != "custom" {
		t.Fatalf("显式 folder 应保留: %q", target2.Folder)
	}
	if FolderNameFor(BundleBinding{}, "pkv") != "bundle/pkv" {
		t.Fatalf("FolderNameFor 默认应为 bundle/pkv")
	}
}
