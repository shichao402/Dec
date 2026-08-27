package secrets

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestSyncTargetDeclaredMarker(t *testing.T) {
	bundle, err := NewBundleSyncTarget("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	machine, err := NewMachineBundleSyncTarget("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := NewProjectSyncTarget("Dec", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]SyncTarget{
		"bundle": bundle, "machine": machine, "project": project, "clone": bundle.Clone(),
	} {
		if !target.Declared() {
			t.Fatalf("%s target 应为 declared", name)
		}
	}
	literal := SyncTarget{Kind: SyncKindBundle, Name: "demo", Folder: "bundle/demo"}
	if literal.Declared() {
		t.Fatal("结构体字面量不得成为 declared target")
	}
	browse, err := NewBrowseFolder("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if browse.Declared() || browse.LocalRoot != "" {
		t.Fatalf("浏览节点必须保持未声明且无 LocalRoot: %#v", browse)
	}
	if err := RequireDeclared(literal); err == nil || !strings.Contains(err.Error(), "ADR 0013") {
		t.Fatalf("RequireDeclared 错误应提及 ADR 0013, got %v", err)
	}
}

func TestSyncTargetJSONPreservesDeclaredMarker(t *testing.T) {
	target, err := NewMachineBundleSyncTarget("demo", "bundle/demo")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var got SyncTarget
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Declared() || got.Plane != SyncPlaneMachine || got.LocalRoot != target.LocalRoot {
		t.Fatalf("JSON round-trip 丢失声明或平面: %#v", got)
	}
}

func TestResolveTargetRebuildsUndeclaredExplicitTarget(t *testing.T) {
	target, err := ResolveTarget(SyncKindBundle, "demo", BundleBinding{}, SyncTarget{
		Kind: SyncKindBundle, Name: "demo", Folder: "bundle/demo", LocalRoot: "handmade",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !target.Declared() {
		t.Fatal("ResolveTarget 应通过构造函数重建未声明 target")
	}
	if target.LocalRoot != ".secrets/bundles/demo" {
		t.Fatalf("ResolveTarget 必须丢弃手搓 LocalRoot, got %q", target.LocalRoot)
	}
}

func TestNewPSyncTargetUsesFixedFolderAndRoots(t *testing.T) {
	project, err := NewPSyncTarget("my-app", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	if project.Folder != "my-app/private/project" || project.LocalRoot != ".secrets/my-app" || !project.Declared() {
		t.Fatalf("project target = %#v", project)
	}
	user, err := NewPSyncTarget("my-app", SyncPlaneUser)
	if err != nil {
		t.Fatal(err)
	}
	if user.Folder != "my-app/private/user" || user.LocalRoot != "my-app" || !IsMachinePlane(user.Plane) {
		t.Fatalf("user target = %#v", user)
	}
	if _, _, ok := ParsePFolder("my-app/public/project"); ok {
		t.Fatal("public folder 不得成为 secrets target")
	}
	if name, plane, ok := ParsePFolder("my-app/private/user"); !ok || name != "my-app" || !IsMachinePlane(plane) {
		t.Fatalf("ParsePFolder = %q %q %v", name, plane, ok)
	}
}
