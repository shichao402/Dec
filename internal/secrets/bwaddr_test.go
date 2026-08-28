package secrets

import "testing"

func TestRemoteScopeFolderIsOnlyPName(t *testing.T) {
	scope, err := NewRemoteScope("dec", SyncPlaneProject)
	if err != nil {
		t.Fatalf("NewRemoteScope: %v", err)
	}
	if got := scope.folderName(); got != "dec" {
		t.Fatalf("folderName = %q，Bitwarden folder 只能是 P 名这一级", got)
	}
	if got := scope.String(); got != "dec/private/project" {
		t.Fatalf("String = %q", got)
	}
}

func TestRemoteScopeItemNameCarriesPlane(t *testing.T) {
	project, err := NewRemoteScope("dec", SyncPlaneProject)
	if err != nil {
		t.Fatalf("NewRemoteScope: %v", err)
	}
	user, err := NewRemoteScope("dec", SyncPlaneUser)
	if err != nil {
		t.Fatalf("NewRemoteScope: %v", err)
	}

	projectName, err := project.encodeItemName("integration/bitwarden.yaml")
	if err != nil {
		t.Fatalf("encodeItemName: %v", err)
	}
	if projectName != "private/project/integration/bitwarden.yaml" {
		t.Fatalf("project 条目名 = %q", projectName)
	}
	userName, err := user.encodeItemName(CanonicalSSHKeyName("deploy"))
	if err != nil {
		t.Fatalf("encodeItemName: %v", err)
	}
	if userName != "private/user/.sshkey/deploy" {
		t.Fatalf("user 条目名 = %q", userName)
	}

	if rel, ok := project.decodeItemName(projectName); !ok || rel != "integration/bitwarden.yaml" {
		t.Fatalf("decodeItemName = %q %v", rel, ok)
	}
	if _, ok := project.decodeItemName(userName); ok {
		t.Fatal("project scope 不能认领 user 平面的条目")
	}
	if _, ok := user.decodeItemName("integration/bitwarden.yaml"); ok {
		t.Fatal("无前缀的历史条目不应被当成本平面资产")
	}
}

func TestParseRemoteScope(t *testing.T) {
	scope, err := ParseRemoteScope("my-app/private/user")
	if err != nil {
		t.Fatalf("ParseRemoteScope: %v", err)
	}
	if scope.P != "my-app" || !IsMachinePlane(scope.Plane) {
		t.Fatalf("scope = %+v", scope)
	}
	for _, bad := range []string{"my-app/public/project", "my-app", "my-app/private", "My-App/private/user", ""} {
		if _, err := ParseRemoteScope(bad); err == nil {
			t.Fatalf("ParseRemoteScope(%q) 应失败", bad)
		}
	}
}

func TestRemoteScopeOfSyncTarget(t *testing.T) {
	target, err := NewPSyncTarget("dec", SyncPlaneUser)
	if err != nil {
		t.Fatalf("NewPSyncTarget: %v", err)
	}
	scope, err := RemoteScopeOf(target)
	if err != nil {
		t.Fatalf("RemoteScopeOf: %v", err)
	}
	if scope.folderName() != "dec" || scope.itemPrefix() != "private/user/" {
		t.Fatalf("scope = %+v prefix=%q", scope, scope.itemPrefix())
	}

	// 存量非 P 地址的浏览节点没有远端寻址域：P 名校验会拦下它。
	browse, err := NewBrowseAddress("bundle/tencent-cloud")
	if err != nil {
		t.Fatalf("NewBrowseAddress: %v", err)
	}
	if _, err := RemoteScopeOf(browse); err == nil {
		t.Fatal("非 P 地址不应产生远端寻址域")
	}
}

func TestBWPlaneSegmentOfItemName(t *testing.T) {
	if plane, ok := bwPlaneSegmentOfItemName("private/project/.env/a.env"); !ok || plane != SyncPlaneProject {
		t.Fatalf("plane = %q %v", plane, ok)
	}
	if plane, ok := bwPlaneSegmentOfItemName("private/user/.gcm/cnb.yaml"); !ok || !IsMachinePlane(plane) {
		t.Fatalf("plane = %q %v", plane, ok)
	}
	for _, bad := range []string{"private/public/a", ".env/a.env", "private/user", ""} {
		if _, ok := bwPlaneSegmentOfItemName(bad); ok {
			t.Fatalf("bwPlaneSegmentOfItemName(%q) 应失败", bad)
		}
	}
}
