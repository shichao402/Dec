package secrets

// SyncKind 区分 project 级与 bundle 级 secrets 目标。
// Bitwarden 上协议完全相同，差别只在本地同步根与 TUI 文案。
type SyncKind string

const (
	SyncKindProject SyncKind = "project"
	SyncKindBundle  SyncKind = "bundle"
)

// SyncPlane 区分本地同步根落在项目内还是机器级 ~/.dec/secrets。
type SyncPlane string

const (
	// SyncPlaneProject 相对当前项目根（默认）。
	SyncPlaneProject SyncPlane = "project"
	// SyncPlaneMachine 相对 ~/.dec/secrets（用户平面）。
	SyncPlaneMachine SyncPlane = "machine"
	// SyncPlaneUser 与 SyncPlaneMachine 同义（ADR 0009 用户平面别名）。
	SyncPlaneUser SyncPlane = "user"
)

// SecretsRootDir 是项目内唯一普通 secret 明文边界。
const SecretsRootDir = ".secrets"

const (
	// ProjectSecretsLocalRel 是 project 级 secrets 相对项目根的同步根。
	ProjectSecretsLocalRel = ".secrets/project"
	// BundleSecretsLocalRelPrefix 是项目内 bundle 级 secrets 同步根前缀：.secrets/bundles/<name>。
	BundleSecretsLocalRelPrefix = ".secrets/bundles"
	// MachineBundleSecretsRelPrefix 是机器级 bundle secrets 相对 ~/.dec/secrets 的前缀：bundles/<name>。
	MachineBundleSecretsRelPrefix = "bundles"
)

// ProjectSecretsDecBundleName 是 project 级 secrets 在内部 API 中使用的占位 Dec bundle 名。
const ProjectSecretsDecBundleName = "_project"

// BundleBinding 描述 Dec bundle 与 Bitwarden folder 的可选别名绑定。
// secrets_bundle 即 Bitwarden folder；未配置时 bundle 默认为 bundle/<dec_bundle>。
type BundleBinding struct {
	DecBundleName     string `yaml:"dec_bundle" json:"dec_bundle"`
	SecretsBundleName string `yaml:"secrets_bundle" json:"secrets_bundle"`
	Folder            string `yaml:"folder,omitempty" json:"folder,omitempty"` // 已废弃，加载时迁移到 secrets_bundle
}

// SyncTarget 是一次 secrets 同步的单位：Bitwarden folder ↔ 本地同步根。
type SyncTarget struct {
	Kind      SyncKind
	Name      string // project_name 或 Dec bundle 名
	Folder    string // Bitwarden folder；bundle 默认 bundle/<name>，project 默认 Name
	LocalRoot string // project 平面：.secrets/...；machine 平面：bundles/<name>（相对 ~/.dec/secrets）
	Plane     SyncPlane
}

// IsMachinePlane 判断是否为用户/机器平面（machine 与 user 同义）。
func IsMachinePlane(plane SyncPlane) bool {
	return plane == SyncPlaneMachine || plane == SyncPlaneUser
}

// SecureNote 表示一条待落地到 SyncTarget.LocalRoot 的 Secure Note。
// RelativePath 是相对 LocalRoot 的路径，也是 Bitwarden Note 名。
type SecureNote struct {
	RelativePath string
	Content      string
}

// SSHKeyItem 表示一条 Bitwarden SSH Key Item。
// Name 是逻辑名（非落地路径）；Hosts 来自 Notes（可选；有内容时一行一个）。
type SSHKeyItem struct {
	ID             string
	Name           string
	Hosts          []string
	PrivateKey     string
	PublicKey      string
	KeyFingerprint string
}

// PullBundleRequest 拉取单个 SyncTarget 的输入。
type PullBundleRequest struct {
	ProjectRoot string
	Target      SyncTarget
	// DecBundleName / Binding 保留兼容旧调用点；优先使用 Target。
	DecBundleName string
	Binding       BundleBinding
}

// PullBundleResult 拉取结果：Secure Notes + SSH Keys。
type PullBundleResult struct {
	Notes   []SecureNote
	SSHKeys []SSHKeyItem
}

// PushBundleRequest 推送单个 SyncTarget 的输入。
type PushBundleRequest struct {
	ProjectRoot   string
	Target        SyncTarget
	DecBundleName string
	Binding       BundleBinding
	// CreateFolderIfMissing 仅用于 Remote 登记新 folder：push 时 folder 不存在则先建。
	// 常规 push / 编辑已有 folder 不设，folder 缺失仍按错误处理。
	CreateFolderIfMissing bool
}

// PushBundleResult 推送结果。
// 无 Deleted：push 不删远端 note，删除只走 Remote 页的显式单条确认。
type PushBundleResult struct {
	Created int
	Updated int
	Paths   []string
	// MissingLocal 是远端有 note、本地缺文件的同步根相对路径。只报告，不删远端。
	MissingLocal []string
}

// DeleteSecureNoteRequest 删除单条 Secure Note 的输入。
type DeleteSecureNoteRequest struct {
	Binding  BundleBinding
	NotePath string // 同步根相对路径（= Note 名）
	Target   SyncTarget
}

// DeleteSSHKeyRequest 删除单条远端 SSH Key 的输入（按逻辑名）。
type DeleteSSHKeyRequest struct {
	Binding BundleBinding
	KeyName string
	Target  SyncTarget
}

// UpdateSSHKeyHostsRequest 更新远端 SSH Key Item Notes（一行一个 Host）。
type UpdateSSHKeyHostsRequest struct {
	Binding BundleBinding
	KeyName string
	Target  SyncTarget
	Hosts   []string // 规范化后写入 Notes；空切片清空 Notes
}

// RenameSecureNoteRequest 将远端 Secure Note 改名（= 改相对同步根路径）。
type RenameSecureNoteRequest struct {
	Binding BundleBinding
	OldPath string
	NewPath string
	Target  SyncTarget
}

// RenameSSHKeyRequest 将远端 SSH Key Item 改名。
type RenameSSHKeyRequest struct {
	Binding BundleBinding
	OldName string
	NewName string
	Target  SyncTarget
}
