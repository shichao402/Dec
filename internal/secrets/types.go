package secrets

// SyncPlane 区分本地同步根落在项目内还是机器级 ~/.dec/secrets。
type SyncPlane string

const (
	SyncPlaneLocal  SyncPlane = "local"
	SyncPlaneGlobal SyncPlane = "global"
	// 旧平面名，仅解析存量地址。
	SyncPlaneProject SyncPlane = "project"
	SyncPlaneMachine SyncPlane = "machine"
	SyncPlaneUser    SyncPlane = "user"
)

// SecretsRootDir 是项目内唯一普通 secret 明文边界。
const SecretsRootDir = ".secrets"

// SyncTarget 是一次 secrets 同步的单位：远端寻址域 ↔ 本地同步根。
//
// Address 是逻辑地址（项目为 <p>/private/<plane>），只用于展示、持久化与跨进程
// 传输。要读写远端必须经 Scope()，由 BW 实现决定真实 folder 名与条目名。
type SyncTarget struct {
	Name      string // 项目名；只读浏览节点可为任意远端名字
	Address   string
	LocalRoot string // project 平面：.secrets/<p>；machine 平面：<p>（相对 ~/.dec/secrets）
	Plane     SyncPlane
	declared  bool // 仅声明型构造函数可置 true；包外字面量只能得到只读/待重建 target
}

// IsMachinePlane 判断是否为用户/机器平面（machine 与 user 同义）。
func IsMachinePlane(plane SyncPlane) bool {
	return plane == SyncPlaneMachine || plane == SyncPlaneUser || plane == SyncPlaneGlobal
}

func CanonicalSyncPlane(plane SyncPlane) SyncPlane {
	if IsMachinePlane(plane) {
		return SyncPlaneGlobal
	}
	return SyncPlaneLocal
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
}

// PullBundleResult 拉取结果：Secure Notes + SSH Keys。
type PullBundleResult struct {
	Notes   []SecureNote
	SSHKeys []SSHKeyItem
}

// PushBundleRequest 推送单个 SyncTarget 的输入。
type PushBundleRequest struct {
	ProjectRoot string
	Target      SyncTarget
	// CreateFolderIfMissing 仅用于 Remote 登记新项目：push 时 folder 不存在则先建。
	// 常规 push / 编辑已有项目不设，folder 缺失仍按错误处理。
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

// CreateSSHKeyRequest 创建一条 Bitwarden SSH Key Item。
// Key.Name 必须是 `.sshkey/<实例>`；CreateFolderIfMissing 仅供 Remote 新项目登记。
type CreateSSHKeyRequest struct {
	Target                SyncTarget
	Key                   SSHKeyItem
	CreateFolderIfMissing bool
}

// DeleteSecureNoteRequest 删除单条 Secure Note 的输入。
type DeleteSecureNoteRequest struct {
	NotePath string // 同步根相对路径
	Target   SyncTarget
}

// DeleteSSHKeyRequest 删除单条远端 SSH Key 的输入（按逻辑名）。
type DeleteSSHKeyRequest struct {
	KeyName string
	Target  SyncTarget
}

// UpdateSSHKeyHostsRequest 更新远端 SSH Key Item Notes（一行一个 Host）。
type UpdateSSHKeyHostsRequest struct {
	KeyName string
	Target  SyncTarget
	Hosts   []string // 规范化后写入 Notes；空切片清空 Notes
}

// RenameSecureNoteRequest 将远端 Secure Note 改名（= 改相对同步根路径）。
type RenameSecureNoteRequest struct {
	OldPath string
	NewPath string
	Target  SyncTarget
}

// RenameSSHKeyRequest 将远端 SSH Key Item 改名。
type RenameSSHKeyRequest struct {
	OldName string
	NewName string
	Target  SyncTarget
}
