package app

import "testing"

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

// 没被官方 pin 订阅的同名项目仍是私仓行，只补远端可用版本供参考。
func TestMergeOfficialCandidates_UnpinnedKeepsVaultIdentity(t *testing.T) {
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
