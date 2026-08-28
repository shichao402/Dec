package secrets

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSyncTargetDeclaredMarker(t *testing.T) {
	project, err := NewPSyncTarget("demo", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := NewPSyncTarget("demo", SyncPlaneUser)
	if err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]SyncTarget{
		"project": project, "machine": machine, "clone": project.Clone(),
	} {
		if !target.Declared() {
			t.Fatalf("%s target 应为 declared", name)
		}
	}
	literal := SyncTarget{Name: "demo", Address: "demo/private/project"}
	if literal.Declared() {
		t.Fatal("结构体字面量不得成为 declared target")
	}
	browse, err := NewBrowseAddress("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if browse.Declared() || browse.LocalRoot != "" {
		t.Fatalf("浏览节点必须保持未声明且无 LocalRoot: %#v", browse)
	}
	if err := RequireDeclared(literal); err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("RequireDeclared 错误应说明未声明, got %v", err)
	}
}

func TestSyncTargetJSONPreservesDeclaredMarker(t *testing.T) {
	target, err := NewPSyncTarget("demo", SyncPlaneUser)
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
	if !got.Declared() || !IsMachinePlane(got.Plane) || got.LocalRoot != target.LocalRoot {
		t.Fatalf("JSON round-trip 丢失声明或平面: %#v", got)
	}
	if got.Address != target.Address {
		t.Fatalf("Address = %q, want %q", got.Address, target.Address)
	}
}

// 手搓的 LocalRoot / Address 不能穿过 JSON 边界：declared target 只能被重建。
func TestSyncTargetJSONRebuildsDeclaredTarget(t *testing.T) {
	raw := []byte(`{"Name":"demo","Address":"demo/private/project","LocalRoot":"handmade","Plane":"project","Declared":true}`)
	var got SyncTarget
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.LocalRoot != ".secrets/demo" {
		t.Fatalf("必须丢弃手搓 LocalRoot, got %q", got.LocalRoot)
	}
}

func TestNewPSyncTargetUsesFixedAddressAndRoots(t *testing.T) {
	project, err := NewPSyncTarget("my-app", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	if project.Address != "my-app/private/local" || project.LocalRoot != ".secrets/my-app" || !project.Declared() {
		t.Fatalf("project target = %#v", project)
	}
	user, err := NewPSyncTarget("my-app", SyncPlaneUser)
	if err != nil {
		t.Fatal(err)
	}
	if user.Address != "my-app/private/global" || user.LocalRoot != "my-app" || !IsMachinePlane(user.Plane) {
		t.Fatalf("user target = %#v", user)
	}
	if _, err := ParseRemoteScope("my-app/public/project"); err == nil {
		t.Fatal("public 象限不得成为 secrets 地址")
	}
	scope, err := ParseRemoteScope("my-app/private/user")
	if err != nil || scope.P != "my-app" || !IsMachinePlane(scope.Plane) {
		t.Fatalf("ParseRemoteScope = %+v err=%v", scope, err)
	}
}
