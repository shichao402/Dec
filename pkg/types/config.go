package types

// VaultConfigV2 项目级 Vault 配置（声明需要从 vault 同步的资产）
type VaultConfigV2 struct {
	VaultSkills []string `yaml:"vault_skills,omitempty"`
	VaultRules  []string `yaml:"vault_rules,omitempty"`
	VaultMCPs   []string `yaml:"vault_mcps,omitempty"`
}
