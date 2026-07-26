package secrets

// BundleBinding 描述 Dec bundle 与 Bitwarden secrets bundle 的绑定关系。
// secrets_bundle 即 Bitwarden folder 查找名；未配置时默认同 dec_bundle。
type BundleBinding struct {
	DecBundleName     string `yaml:"dec_bundle" json:"dec_bundle"`
	SecretsBundleName string `yaml:"secrets_bundle" json:"secrets_bundle"`
	Folder            string `yaml:"folder,omitempty" json:"folder,omitempty"` // 已废弃，加载时迁移到 secrets_bundle
	TargetDir         string `yaml:"target_dir" json:"target_dir"`
}

// SecureNote 表示一条待落地到项目根的 Secure Note。
type SecureNote struct {
	RelativePath string
	Content      string
}

// PullBundleRequest 拉取单个 secrets bundle 的输入。
type PullBundleRequest struct {
	ProjectRoot   string
	DecBundleName string
	Binding       BundleBinding
}

// PullBundleResult 拉取结果（不含 SSH Key，后续扩展）。
type PullBundleResult struct {
	Notes []SecureNote
}

// PushBundleRequest 推送单个 secrets bundle 的输入。
type PushBundleRequest struct {
	ProjectRoot   string
	DecBundleName string
	Binding       BundleBinding
}

// PushBundleResult 推送结果。
// 无 Deleted：push 不删远端 note，删除只走 Delete 页的显式单条确认。
type PushBundleResult struct {
	Created int
	Updated int
	Paths   []string
	// MissingLocal 是远端有 note、本地缺文件的落地路径。只报告，不删远端。
	MissingLocal []string
}

// DeleteSecureNoteRequest 删除单条 Secure Note 的输入。
type DeleteSecureNoteRequest struct {
	Binding  BundleBinding
	NotePath string
}
