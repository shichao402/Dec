package app

import (
	"testing"
)

// 官方 pin 的项目在私仓里也有同名目录时，行必须按官方身份下发：
// 否则面板标「私仓」、藏掉版本与「有更新」，CI 发了新版用户也看不到。
func TestMergeOfficialCandidates_OfficialPinWinsOverVaultRow(t *testing.T) {
	vault := []AssetBundleOption{{
		Name:    "relkit",
		Vault:   "relkit",
		Source:  AssetSourceVault,
		Enabled: true, // 被别人的 depends_on 带进来，不代表订阅
		Model:   "p",
	}}
	official := []AssetBundleOption{{
		Name:            "relkit",
		Source:          AssetSourceOfficial,
		Pin:             "latest",
		Installed:       "v0.4.2",
		Available:       "v0.4.4",
		Enabled:         true,
		Required:        true,
		UpdateAvailable: true,
	}}

	merged := mergeOfficialCandidates(vault, official)
	if len(merged) != 1 {
		t.Fatalf("同名项目应只留一行，merged = %+v", merged)
	}
	got := merged[0]
	if got.Source != AssetSourceOfficial {
		t.Errorf("Source = %q, want %q", got.Source, AssetSourceOfficial)
	}
	if got.Pin != "latest" {
		t.Errorf("Pin = %q, want latest", got.Pin)
	}
	if got.Installed != "v0.4.2" || got.Available != "v0.4.4" {
		t.Errorf("版本 = %q/%q, want v0.4.2/v0.4.4", got.Installed, got.Available)
	}
	if !got.UpdateAvailable {
		t.Error("UpdateAvailable 应为 true，否则「有更新」标记与更新页提示都不会出现")
	}
	if !got.Required {
		t.Error("Required 应为 true：它在 requires 里")
	}
}

// 显式 vault pin 是用户的选择，仍是私仓行，只补远端可用版本供参考。
func TestMergeOfficialCandidates_VaultPinKeepsVaultIdentity(t *testing.T) {
	vault := []AssetBundleOption{{Name: "relkit", Source: AssetSourceVault, Pin: "vault", Enabled: true}}
	official := []AssetBundleOption{{Name: "relkit", Source: AssetSourceOfficial, Available: "v0.4.4"}}

	merged := mergeOfficialCandidates(vault, official)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Source != AssetSourceVault || merged[0].Pin != "vault" {
		t.Errorf("私仓 pin 不应被官方行改写：%+v", merged[0])
	}
	if merged[0].Available != "v0.4.4" {
		t.Errorf("Available = %q, want v0.4.4", merged[0].Available)
	}
	if merged[0].UpdateAvailable {
		t.Error("私仓行没有版本概念，不该报「有更新」")
	}
	if !merged[0].VaultAvailable || !merged[0].OfficialAvailable {
		t.Errorf("两边都有的行必须标出双来源，否则面板没有切回官方的入口：%+v", merged[0])
	}
}

// 还没订阅的同名项目按官方身份下发。默认私仓会锁死这一类项目：
// 勾出来只能是 vault pin，而「更新」页只认官方 pin，于是永远更新不到。
func TestMergeOfficialCandidates_UnsubscribedPrefersOfficial(t *testing.T) {
	vault := []AssetBundleOption{{Name: "relkit", Source: AssetSourceVault, Model: "p"}}
	official := []AssetBundleOption{{Name: "relkit", Source: AssetSourceOfficial, Available: "v0.4.9"}}

	merged := mergeOfficialCandidates(vault, official)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	got := merged[0]
	if got.Source != AssetSourceOfficial {
		t.Errorf("Source = %q, want %q", got.Source, AssetSourceOfficial)
	}
	if got.Pin != "" {
		t.Errorf("Pin = %q, 未订阅的行不该被填上 pin", got.Pin)
	}
	if got.Available != "v0.4.9" {
		t.Errorf("Available = %q, want v0.4.9", got.Available)
	}
	if !got.VaultAvailable || !got.OfficialAvailable {
		t.Errorf("双来源标记缺失：%+v", got)
	}
}

// 家项目从工作树创作，即便注册表也发布了同名项目，也不能被改写成官方行。
func TestMergeOfficialCandidates_HomeStaysVault(t *testing.T) {
	vault := []AssetBundleOption{{Name: "relkit", Source: AssetSourceVault, Home: true, Enabled: true}}
	official := []AssetBundleOption{{Name: "relkit", Source: AssetSourceOfficial, Available: "v0.4.9"}}

	merged := mergeOfficialCandidates(vault, official)
	if merged[0].Source != AssetSourceVault {
		t.Errorf("家项目 Source = %q, want %q", merged[0].Source, AssetSourceVault)
	}
	if merged[0].Pin != "" {
		t.Errorf("家项目不进 requires，Pin 应为空，实际 %q", merged[0].Pin)
	}
}

// 私仓里没有的官方项目独立成行。
func TestMergeOfficialCandidates_AppendsUnknownProjects(t *testing.T) {
	merged := mergeOfficialCandidates(
		[]AssetBundleOption{{Name: "woa", Source: AssetSourceVault}},
		[]AssetBundleOption{{Name: "relkit", Source: AssetSourceOfficial, Available: "v0.4.4"}},
	)
	if len(merged) != 2 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[1].Name != "relkit" || merged[1].Source != AssetSourceOfficial {
		t.Errorf("官方项目应独立成行：%+v", merged[1])
	}
}

// 私仓 pin 的行仍是私仓身份，但注册表里的源仓和 Git 资产要挂上来。
// 否则订阅页只剩密钥名单，看不出这个产品已经从产品仓发布。
func TestMergeOfficialCandidates_ShowsPublishedAssetsOnVaultRow(t *testing.T) {
	vault := []AssetBundleOption{{
		Name:        "woa",
		Source:      AssetSourceVault,
		Pin:         "vault",
		Description: "secrets-only / machine-enabled placeholder (ADR 0003)",
		Tags:        []string{"global"},
		Members:     []AssetSelectionItem{{Name: ".sshkey/devcloud", Type: AssetMemberTypeSecret}},
	}}
	official := []AssetBundleOption{{
		Name:       "woa",
		Source:     AssetSourceOfficial,
		Available:  "v0.1.0+1",
		OriginRepo: "https://github.com/shichao402/DecPersonalDevKit.git",
		Members:    []AssetSelectionItem{{Name: "gongfeng", Type: "skill"}, {Name: "gongfeng", Type: "mcp"}},
	}}

	merged := mergeOfficialCandidates(vault, official)
	got := merged[0]
	if got.Source != AssetSourceVault || got.Pin != "vault" {
		t.Fatalf("私仓 pin 被改写：%+v", got)
	}
	if got.OriginRepo != official[0].OriginRepo {
		t.Fatalf("origin = %q", got.OriginRepo)
	}
	if got.Description != "" {
		t.Fatalf("密钥占位说明应让位，description = %q", got.Description)
	}
	if len(got.Members) != 3 || got.Members[0].Type != "skill" || got.Members[2].Type != AssetMemberTypeSecret {
		t.Fatalf("members = %#v", got.Members)
	}
	if len(got.Tags) != 0 {
		t.Fatalf("未发布推荐标签时，不该沿用私仓 stub 上的标签：%#v", got.Tags)
	}
}

// 官方行带来的 origin_repo 是提 Issue / PR 的落脚点，合并时不能丢。
func TestMergeOfficialCandidates_KeepsOriginRepo(t *testing.T) {
	vault := []AssetBundleOption{{Name: "playbook", Source: AssetSourceVault}}
	official := []AssetBundleOption{{Name: "playbook", Source: AssetSourceOfficial, OriginRepo: "https://github.com/a/b", Pin: "latest"}}
	merged := mergeOfficialCandidates(vault, official)
	if merged[0].OriginRepo != "https://github.com/a/b" {
		t.Errorf("origin lost: %+v", merged[0])
	}
	if merged[0].Source != AssetSourceOfficial {
		t.Errorf("pin latest should be official: %q", merged[0].Source)
	}
}

func TestMergeOfficialCandidates_IdentityProductClearsSecretsPlaceholder(t *testing.T) {
	vault := []AssetBundleOption{{
		Name:        "github",
		Description: "secrets-only / machine-enabled placeholder (ADR 0003)",
		Tags:        []string{"global"},
		Pin:         "vault",
		Source:      AssetSourceVault,
		Members:     []AssetSelectionItem{{Name: ".env/github.env", Type: AssetMemberTypeSecret}},
	}}
	official := []AssetBundleOption{{
		Name:       "github",
		Source:     AssetSourceOfficial,
		OriginRepo: "https://github.com/shichao402/DecPersonalDevKit.git",
		Tags:       []string{"global"},
		Available:  "v0.1.0+2",
	}}
	got := mergeOfficialCandidates(vault, official)[0]
	if got.Description != "" {
		t.Fatalf("description = %q", got.Description)
	}
	if got.OriginRepo != official[0].OriginRepo || got.Pin != "vault" || got.Source != AssetSourceVault {
		t.Fatalf("identity merge = %+v", got)
	}
	if len(got.Members) != 1 || got.Members[0].Type != AssetMemberTypeSecret {
		t.Fatalf("members = %#v", got.Members)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "global" {
		t.Fatalf("tags = %#v", got.Tags)
	}
}
