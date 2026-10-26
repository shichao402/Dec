// Package ssot 定义 Dec Cloud 单一数据源（SSOT）的磁盘布局与数据类型。
//
// 仓库内只有四类内容：
//
//	assets/<bundle>/...            资产（skill 目录、rule、MCP 条目、env 模板）
//	secrets/<name>.json            按值加密后的密钥
//	config/devices.json            设备登记、设备类型、项目登记与订阅
//	config/projects.json           项目登记（设备、本机路径、订阅的 bundle）
//
// 加密后的密钥可以安全地进入 git 历史，明文永远不进仓库。
package ssot

import "time"

// 目录与文件名约定。
const (
	DirAssets  = "assets"
	DirSecrets = "secrets"
	DirConfig  = "config"

	FileDevices  = DirConfig + "/devices.json"
	FileProjects = DirConfig + "/projects.json"
	FileManifest = "manifest.yaml"
	// FileRootKeyCheck 记录当前根密钥的 key id 与校验值（需求 9.3、11.2）。
	//
	// 它只放 key id 与材料摘要，绝不放密钥本身：这个文件进 git、进冷备，
	// 而需求 9.2 明令根密钥本身不得进入这两处。有它的价值在于
	// 服务器重建后能回答“当前该用哪一把根密钥”，否则只能从一堆
	// root-key/<key-id> 冷备条目里挑一个最新的——挑错了就会解不开旧密文。
	FileRootKeyCheck = DirConfig + "/rootkey.json"

	// NamespaceCloud 是 Dec Cloud 服务端自己占用的 bundle 命名空间，
	// 用户资产不得使用，以免与冷备、审计等内置内容冲突。
	NamespaceCloud = "_cloud"

	// DirRootKeyBackup 是 Bitwarden 里保存根密钥冷备的保留 folder。
	// 任何自动流程都不得删除或改写它。
	DirRootKeyBackup = "_dec-cloud"
	// FolderRootKey 是保留 folder 下的条目前缀：root-key/<key-id>。
	FolderRootKey = "root-key"
)

// AssetKind 是资产类型。
type AssetKind string

const (
	// KindSkill 是目录型资产，整个子目录随 bundle 下发。
	KindSkill AssetKind = "skill"
	// KindRule 是单文件文本资产。
	KindRule AssetKind = "rule"
	// KindMCP 是 MCP 服务条目，渲染进 IDE/Agent 的 MCP 配置文件。
	KindMCP AssetKind = "mcp"
	// KindEnv 是环境变量模板，渲染时用密钥填充。
	KindEnv AssetKind = "env"
)

// Valid 报告资产类型是否受支持。
func (k AssetKind) Valid() bool {
	switch k {
	case KindSkill, KindRule, KindMCP, KindEnv:
		return true
	}
	return false
}

// BundleManifest 是 assets/<bundle>/manifest.yaml 的结构。
type BundleManifest struct {
	// Name 必须与所在目录名一致。
	Name string `yaml:"name" json:"name"`
	// Revision 是写入方看到的版本号，服务端用它做乐观并发控制。
	Revision string `yaml:"revision,omitempty" json:"revision,omitempty"`
	// Items 是本 bundle 声明的全部托管条目。
	Items []ManifestItem `yaml:"items" json:"items"`
}

// ManifestItem 描述一条托管条目。删除一条资产就是把它从 manifest 里移除。
type ManifestItem struct {
	// Kind 是资产类型。
	Kind AssetKind `yaml:"kind" json:"kind"`
	// Path 是仓库内相对 assets/<bundle>/ 的路径。skill 类型指向目录。
	Path string `yaml:"path" json:"path"`
	// Name 是渲染后的条目名，用于在共享配置文件里生成 dec-<name> 这类唯一标识。
	Name string `yaml:"name" json:"name"`
	// Planes 声明这条资产属于哪些平面：user 和/或 project。
	Planes []Plane `yaml:"planes" json:"planes"`
	// Host 是 mcp 类型的可选渲染目标，例如 cursor、codebuddy。空表示全部已启用宿主。
	Host []string `yaml:"host,omitempty" json:"host,omitempty"`
}

// Plane 是落点平面。
type Plane string

const (
	// PlaneUser 落在用户目录下的 IDE/Agent 全局位置。
	PlaneUser Plane = "user"
	// PlaneProject 落在已登记项目根目录下的 IDE/Agent 目录。
	PlaneProject Plane = "project"
)

// Valid 报告平面取值是否受支持。
func (p Plane) Valid() bool {
	return p == PlaneUser || p == PlaneProject
}

// DeviceKind 是设备类型。设备身份即设备证书，不存在用户名口令。
type DeviceKind string

const (
	// DeviceMine 是“我的设备”，拥有全部能力。
	DeviceMine DeviceKind = "mine"
	// DeviceReceiver 是“只接收设备”，无人值守，只接收下发。
	DeviceReceiver DeviceKind = "receiver"
)

// Valid 报告设备类型是否受支持。
func (k DeviceKind) Valid() bool {
	return k == DeviceMine || k == DeviceReceiver
}

// Device 是设备登记表里的一条记录。
type Device struct {
	// ID 是服务端生成的稳定标识。
	ID string `json:"id"`
	// Name 是用户可读的名字。
	Name string `json:"name"`
	// Kind 是设备类型。
	Kind DeviceKind `json:"kind"`
	// OS 与 Arch 用于选择客户端二进制。
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// Fingerprint 是设备公钥的 SHA-256 指纹，用于证书续签时确认同一把密钥。
	Fingerprint string `json:"fingerprint"`
	// CertSerial 是当前有效证书的序列号，吊销后作废。
	CertSerial string `json:"cert_serial"`
	// CertNotAfter 是证书到期时间，到期前一小时允许自动续签。
	CertNotAfter time.Time `json:"cert_not_after"`
	// Revoked 标记设备已吊销。吊销后不再接受任何请求，也不再下发。
	Revoked bool `json:"revoked"`
	// RevokedAt 是吊销时间。
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	// RegisteredAt 是登记时间。
	RegisteredAt time.Time `json:"registered_at"`
	// ReportedVersion 是设备最近一次上报的代理版本。
	ReportedVersion string `json:"reported_version,omitempty"`
	// LastSeen 是最后一次成功交互的时间，用于在线状态与断连告警。
	LastSeen *time.Time `json:"last_seen,omitempty"`
	// UserBundles 是这台设备用户平面订阅的 bundle。
	UserBundles []string `json:"user_bundles,omitempty"`
}

// Project 是项目登记：某台设备上的某个项目根目录订阅了哪些 bundle。
type Project struct {
	// ID 是服务端生成的稳定标识。
	ID string `json:"id"`
	// DeviceID 是所属设备。
	DeviceID string `json:"device_id"`
	// Name 是项目名，用于在管理面区分。
	Name string `json:"name"`
	// Path 是本机绝对路径，必须位于用户目录之下且已存在。
	Path string `json:"path"`
	// Bundles 是这个项目订阅的 bundle。
	Bundles []string `json:"bundles"`
	// RegisteredAt 是登记时间。
	RegisteredAt time.Time `json:"registered_at"`
}

// DeviceTable 是 config/devices.json 的结构。
type DeviceTable struct {
	SchemaVersion int      `json:"schema_version"`
	Devices       []Device `json:"devices"`
}

// ProjectTable 是 config/projects.json 的结构。
type ProjectTable struct {
	SchemaVersion int       `json:"schema_version"`
	Projects      []Project `json:"projects"`
}

// RootKeyCheck 是 config/rootkey.json 的结构（需求 9.3、11.2）。
//
// 它回答两个问题：当前该用哪把根密钥，以及本地那把钥匙是不是它。
// 恢复时按 KeyID 去冷备里取对应条目，取不到就拒绝恢复（需求 11.3），
// 而不是拿“最新的一条”凑合——轮换过的历史里，最新未必是当前在用的那把。
type RootKeyCheck struct {
	SchemaVersion int `json:"schema_version"`
	// KeyID 是当前根密钥的 key id。
	KeyID string `json:"key_id"`
	// Digest 是根密钥材料的 SHA-256，用于确认本地文件没被换成另一把钥匙。
	//
	// 它不是密钥派生：只知道摘要无法还原 256 位随机材料，
	// 所以放进 git 不会降低根密钥的保密性。
	Digest string `json:"digest"`
	// CreatedAt 是这把根密钥的生成时间。
	CreatedAt time.Time `json:"created_at"`
	// RotatedFrom 是轮换前的 key id；首次生成时为空。
	RotatedFrom string `json:"rotated_from,omitempty"`
}

// SchemaVersion 是当前配置文件的结构版本。
const SchemaVersion = 1
