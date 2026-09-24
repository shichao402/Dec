package app

import (
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

// 已订阅的项目在私仓里也有同名目录时，行必须按注册表身份下发：
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

// 已发布项目上残留的 vault pin 不是第二种安装来源，行必须改成官方并跟随 latest。
func TestMergeOfficialCandidates_PublishedVaultPinBecomesOfficial(t *testing.T) {
	vault := []AssetBundleOption{{Name: "relkit", Source: AssetSourceVault, Pin: "vault", Enabled: true}}
	official := []AssetBundleOption{{Name: "relkit", Source: AssetSourceOfficial, Available: "v0.4.4"}}

	merged := mergeOfficialCandidates(vault, official)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Source != AssetSourceOfficial || merged[0].Pin != types.RequiresLatest {
		t.Errorf("已发布项目不应留在私仓 pin：%+v", merged[0])
	}
	if merged[0].Available != "v0.4.4" {
		t.Errorf("Available = %q, want v0.4.4", merged[0].Available)
	}
	if !merged[0].UpdateAvailable {
		t.Error("已订阅的已发布项目要露出可用版本")
	}
}

// 还没订阅的同名项目按官方身份下发。私仓里的同名目录不构成另一个安装来源。
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
}

// 本仓项目从工作树创作，即便注册表也发布了同名项目，也不能被改写成官方行。
func TestMergeOfficialCandidates_HomeStaysVault(t *testing.T) {
	vault := []AssetBundleOption{{Name: "relkit", Source: AssetSourceVault, Home: true, Enabled: true}}
	official := []AssetBundleOption{{Name: "relkit", Source: AssetSourceOfficial, Available: "v0.4.9"}}

	merged := mergeOfficialCandidates(vault, official)
	if merged[0].Source != AssetSourceVault {
		t.Errorf("本仓项目 Source = %q, want %q", merged[0].Source, AssetSourceVault)
	}
	if merged[0].Pin != "" {
		t.Errorf("本仓项目不进 requires，Pin 应为空，实际 %q", merged[0].Pin)
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

// 已发布项目即使 requires 还写着 vault，行也是官方身份，并带上注册表里的源仓和资产。
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
	if got.Source != AssetSourceOfficial || got.Pin != types.RequiresLatest {
		t.Fatalf("已发布项目应改为官方 latest：%+v", got)
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

func TestSubscriptionCandidateRowsDropsVaultProjects(t *testing.T) {
	rows := subscriptionCandidateRows([]AssetBundleOption{
		{Name: "notes", Source: AssetSourceVault},
		{Name: "dec", Source: AssetSourceVault, Home: true},
		{Name: "relkit", Source: AssetSourceOfficial, Pin: "latest"},
		{Name: "orphan", SecretsOnly: true},
	})
	if len(rows) != 2 || rows[0].Name != "dec" || rows[1].Name != "relkit" {
		t.Fatalf("rows = %+v", rows)
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
	if got.OriginRepo != official[0].OriginRepo || got.Pin != types.RequiresLatest || got.Source != AssetSourceOfficial {
		t.Fatalf("identity merge = %+v", got)
	}
	if len(got.Members) != 1 || got.Members[0].Type != AssetMemberTypeSecret {
		t.Fatalf("members = %#v", got.Members)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "global" {
		t.Fatalf("tags = %#v", got.Tags)
	}
}
