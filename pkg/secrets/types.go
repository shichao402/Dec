package secrets

// BundleBinding 描述 Dec bundle 与 Bitwarden secrets bundle 的绑定关系。
type BundleBinding struct {
	DecBundleName     string `yaml:"dec_bundle" json:"dec_bundle"`
	SecretsBundleName string `yaml:"secrets_bundle" json:"secrets_bundle"`
	BitwardenFolder   string `yaml:"folder" json:"folder"`
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
