package bundle

import (
	"testing"
)

func TestVaultAssetKinds_CoversKnownTypes(t *testing.T) {
	wantTypes := []string{"skill", "command", "rule", "mcp"}
	wantDirs := []string{"skills", "commands", "rules", "mcp"}

	if len(VaultAssetKinds) != len(wantTypes) {
		t.Fatalf("VaultAssetKinds len = %d, want %d — 增删类型时请同步本断言与 schema AssetType", len(VaultAssetKinds), len(wantTypes))
	}
	gotTypes := map[string]bool{}
	gotDirs := map[string]bool{}
	for _, k := range VaultAssetKinds {
		if k.Dir == "" || k.Type == "" {
			t.Fatalf("VaultAssetKind 缺少 Dir/Type: %+v", k)
		}
		if k.DirEntries && k.Suffix != "" {
			t.Fatalf("目录型资产不应带 Suffix: %+v", k)
		}
		if !k.DirEntries && k.Suffix == "" {
			t.Fatalf("文件型资产必须带 Suffix: %+v", k)
		}
		gotTypes[k.Type] = true
		gotDirs[k.Dir] = true
	}
	for _, typ := range wantTypes {
		if !gotTypes[typ] {
			t.Fatalf("VaultAssetKinds 缺少类型 %q", typ)
		}
	}
	for _, dir := range wantDirs {
		if !gotDirs[dir] {
			t.Fatalf("VaultAssetKinds 缺少目录 %q", dir)
		}
	}

	if got := VaultAssetDirs(); len(got) != len(wantDirs) {
		t.Fatalf("VaultAssetDirs() = %v", got)
	}
	for i, dir := range wantDirs {
		if VaultAssetDirs()[i] != dir {
			t.Fatalf("VaultAssetDirs()[%d] = %q, want %q", i, VaultAssetDirs()[i], dir)
		}
	}

	for i, typ := range wantTypes {
		if ValidMemberTypes[i] != typ {
			t.Fatalf("ValidMemberTypes[%d] = %q, want %q", i, ValidMemberTypes[i], typ)
		}
	}
}

func TestTypeSubDirAndDirToType(t *testing.T) {
	cases := []struct {
		typ string
		dir string
	}{
		{"skill", "skills"},
		{"command", "commands"},
		{"rule", "rules"},
		{"mcp", "mcp"},
	}
	for _, tc := range cases {
		if got := TypeSubDir(tc.typ); got != tc.dir {
			t.Fatalf("TypeSubDir(%q) = %q, want %q", tc.typ, got, tc.dir)
		}
		if got := DirToType(tc.dir); got != tc.typ {
			t.Fatalf("DirToType(%q) = %q, want %q", tc.dir, got, tc.typ)
		}
	}
	if TypeSubDir("agents") != "" || DirToType("hooks") != "" {
		t.Fatal("未知类型/目录应返回空串")
	}
}

func TestAssetEntryAndFileName(t *testing.T) {
	rule, ok := KindByType("rule")
	if !ok {
		t.Fatal("缺少 rule kind")
	}
	if got := AssetEntryName(rule, "foo.mdc"); got != "foo" {
		t.Fatalf("AssetEntryName(rule) = %q", got)
	}
	if got := AssetFileName(rule, "foo"); got != "foo.mdc" {
		t.Fatalf("AssetFileName(rule) = %q", got)
	}

	skill, ok := KindByType("skill")
	if !ok {
		t.Fatal("缺少 skill kind")
	}
	if got := AssetFileName(skill, "bar"); got != "bar" {
		t.Fatalf("AssetFileName(skill) = %q", got)
	}
}
