// Package proto 定义 Dec Cloud 代理与服务端之间的线协议。
//
// 协议是这一整套架构里唯一必须长期稳定的东西：客户端一次安装之后不再调整，
// 所以任何一次不兼容的改动都要走需求 19 的版本对齐流程。
//
// 约定：
//
//   - 消息体是 JSON，字段一律带 json tag，新增字段只能追加，不能改旧字段语义。
//   - 每个请求都带 ProtocolVersion，服务端据此决定是否要求升级。
//   - 传输是“单一 HTTPS 端口 + 长连接上的请求/响应”，不是通用 RPC，
//     代理只能调用下面这些方法，服务端也只会下发下面这些类型的内容。
package proto

import "time"

// Version 是当前协议版本。
// 只有在做不兼容改动时才提升；提升时必须同步提升发布清单里的要求版本（需求 19.2）。
const Version = 1

// Method 是协议方法名。
type Method string

const (
	// MethodHello 建立连接并上报版本。永不破坏兼容（需求 19.7）。
	MethodHello Method = "hello"
	// MethodSync 对账：请求补齐、上报本地清单哈希、上报本地改动。
	MethodSync Method = "sync"
	// MethodAck 回报一个 batch 的落地结果。
	MethodAck Method = "ack"
	// MethodEnroll 登记新设备。这是无设备证书可访问的方法之一（需求 18.4）。
	MethodEnroll Method = "enroll"
	// MethodRecover 恢复模式下取回根密钥并登记第一台设备（需求 11）。
	MethodRecover Method = "recover"
	// MethodWriteAsset 同步写入资产（需求 7）。
	MethodWriteAsset Method = "write_asset"
	// MethodDeleteAsset 删除一个资产条目（需求 7.4）。
	MethodDeleteAsset Method = "delete_asset"
	// MethodPutSecret 写入或更新密钥（需求 8）。
	MethodPutSecret Method = "put_secret"
	// MethodDeleteSecret 删除密钥（需求 8.4）。
	MethodDeleteSecret Method = "delete_secret"
	// MethodReadSecret 读取密钥明文，必须记入审计（需求 8.6）。
	MethodReadSecret Method = "read_secret"
	// MethodListSecrets 列出密钥元数据，不含明文。
	MethodListSecrets Method = "list_secrets"
	// MethodListAssets 列出 bundle 与条目。
	MethodListAssets Method = "list_assets"
	// MethodReadAsset 读取一个 bundle 的完整内容与 manifest。
	//
	// list_assets 只给概要（条目数、类型），管理面要“浏览资产”就必须能看内容。
	// 单独一个方法而不是让 list_assets 返回全文：内容总量可能是几十 MB，
	// 列表页不该为了一次点击把全部内容拉下来。
	MethodReadAsset Method = "read_asset"
	// MethodHistory 查询资产或密钥的历史（需求 14.1）。
	MethodHistory Method = "history"
	// MethodRollback 回滚到某个历史 revision（需求 1.4）。
	MethodRollback Method = "rollback"
	// MethodDevices 设备列表与在线状态（需求 14.1）。
	MethodDevices Method = "devices"
	// MethodRevokeDevice 吊销设备（需求 18.7）。
	MethodRevokeDevice Method = "revoke_device"
	// MethodSetDeviceKind 切换设备类型（需求 18.2）。
	MethodSetDeviceKind Method = "set_device_kind"
	// MethodRotationList 吊销后的密钥轮换清单（需求 18.11）。
	MethodRotationList Method = "rotation_list"
	// MethodProjects 项目登记与订阅（需求 23.3）。
	MethodProjects Method = "projects"
	// MethodPutProject 新增或修改项目登记。
	MethodPutProject Method = "put_project"
	// MethodDeleteProject 删除项目登记。
	MethodDeleteProject Method = "delete_project"
	// MethodSetSubscriptions 修改设备的用户平面订阅。
	MethodSetSubscriptions Method = "set_subscriptions"
	// MethodOverrides 本地改动覆盖记录（需求 6.2）。
	MethodOverrides Method = "overrides"
	// MethodRotateRootKey 轮换根密钥（需求 9.4）。
	MethodRotateRootKey Method = "rotate_root_key"
	// MethodStatus 服务状态：要求版本、冷备校验时间、维护状态等。
	MethodStatus Method = "status"
)

// Source 是请求来源，用于权限判定和审计（需求 13.2、已定决策 12）。
type Source string

const (
	// SourceAgent 表示请求来自本机代理自身（系统服务触发的对账、上报）。
	SourceAgent Source = "agent"
	// SourceMCP 表示请求来自本机 Agent（AI）经 MCP 入口发起。
	// MCP 来源永远看不到明文，也不能管理设备。
	SourceMCP Source = "mcp"
	// SourceAdmin 表示请求来自本机页面的管理面转发（用户本人在浏览器里操作）。
	SourceAdmin Source = "admin"
	// SourceEnroll 表示登记/恢复流程，此时还没有设备身份。
	SourceEnroll Source = "enroll"
	// SourceHostCLI 表示无桌面环境下的 CLI 操作。
	SourceHostCLI Source = "hostcli"
)

// Request 是所有请求的统一外壳。
type Request struct {
	ID       string `json:"id"`
	Protocol int    `json:"protocol"`
	Method   Method `json:"method"`
	Source   Source `json:"source,omitempty"`
	// Payload 是对应方法的结构体，由调用方按方法自行序列化。
	Payload []byte `json:"payload,omitempty"`
	// RequestedVersion 是代理当前版本，每个请求都带，便于服务端做版本拦截（需求 19.4）。
	RequestedVersion string `json:"requested_version,omitempty"`
}

// ErrorCode 是稳定错误码，代理据此决定提示文案，不解析错误文本。
type ErrorCode string

const (
	// ErrVersionMismatch 表示代理版本与要求版本不一致，只允许上报版本和获取要求版本。
	ErrVersionMismatch ErrorCode = "version_mismatch"
	// ErrUnauthorized 表示设备身份无效、过期或已吊销。
	ErrUnauthorized ErrorCode = "unauthorized"
	// ErrForbidden 表示来源不允许该操作，例如 MCP 试图读取明文。
	ErrForbidden ErrorCode = "forbidden"
	// ErrConflict 表示并发冲突，例如 base revision 已过期。
	ErrConflict ErrorCode = "conflict"
	// ErrInvalid 表示参数不合规。
	ErrInvalid ErrorCode = "invalid"
	// ErrNotFound 表示对象不存在。
	ErrNotFound ErrorCode = "not_found"
	// ErrInUse 表示对象仍被引用，不能删除。
	ErrInUse ErrorCode = "in_use"
	// ErrMaintenance 表示服务处于维护状态（需求 21.4）。
	ErrMaintenance ErrorCode = "maintenance"
	// ErrBootstrapClosed 表示初始化入口已关闭（需求 10.3）。
	ErrBootstrapClosed ErrorCode = "bootstrap_closed"
	// ErrRateLimited 表示触发失败次数限制或冷却（需求 10.7、18.6）。
	ErrRateLimited ErrorCode = "rate_limited"
	// ErrInternal 表示服务端内部错误。
	ErrInternal ErrorCode = "internal"
)

// Response 是所有响应的统一外壳。
type Response struct {
	ID    string    `json:"id"`
	OK    bool      `json:"ok"`
	Code  ErrorCode `json:"code,omitempty"`
	Error string    `json:"error,omitempty"`
	// RequiredVersion 在版本不匹配时返回，便于代理直接提示（需求 19.5）。
	RequiredVersion string `json:"required_version,omitempty"`
	Payload         []byte `json:"payload,omitempty"`
}

// OK 构造成功响应。
func OK(id string, payload []byte) *Response {
	return &Response{ID: id, OK: true, Payload: payload}
}

// Fail 构造失败响应。
func Fail(id string, code ErrorCode, msg string) *Response {
	return &Response{ID: id, OK: false, Code: code, Error: msg}
}

// HelloRequest 是建立连接后的第一帧。
type HelloRequest struct {
	AgentVersion string `json:"agent_version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	// DeviceName 让服务端在管理面显示可读名字，代理首次运行时取主机名。
	DeviceName string `json:"device_name"`
}

// HelloResponse 返回服务端认为代理应该处于的状态。
type HelloResponse struct {
	ServerVersion   string `json:"server_version"`
	RequiredVersion string `json:"required_version"`
	// ProtocolVersion 让代理在握手阶段就能发现协议版本不兼容。
	ProtocolVersion int `json:"protocol_version"`
	// Maintenance 表示服务正在部署，代理应进入离线降级（需求 21.4）。
	Maintenance bool `json:"maintenance"`
	// Aligned 表示版本是否对齐。未对齐时代理只应提示更新，不再申请下发。
	Aligned bool `json:"aligned"`
	// DeviceID 回显，便于代理确认自己登记的是哪台设备。
	DeviceID string `json:"device_id"`
}

// Op 是下发操作的类型。代理只有这两种原语（需求 2.2）。
type Op string

const (
	// OpWrite 表示写入一个落点。内容已渲染完成，密钥已内联。
	OpWrite Op = "write"
	// OpDelete 表示删除一个由 Dec 管理的落点或条目。
	OpDelete Op = "delete"
)

// Plan 是落点平面。
type Plan string

const (
	PlanUser    Plan = "user"
	PlanProject Plan = "project"
)

// Target 是下发目标。服务端只表达“逻辑位置”，真实路径由代理按自己的目录约定展开，
// 因为不同设备的项目路径不同（需求 23.3）。
type Target struct {
	// Plan 是用户平面还是项目平面。
	Plan Plan `json:"plan"`
	// ProjectID 在项目平面下必填，指向代理本地的项目登记。
	ProjectID string `json:"project_id,omitempty"`
	// Root 是用户平面下的相对根，例如 "cursor"、"codebuddy"、"dec"。
	Root string `json:"root"`
	// Path 是相对 root 的路径。
	Path string `json:"path"`
}

// File 是一个待落盘的托管文件。
type File struct {
	// ID 是稳定标识，用于回执比对：asset:<bundle>:<item> 或 secret:<name>。
	ID string `json:"id"`
	// Plan 与 Target 描述落点。按条目下发而不是按文件下发，
	// 因为一个 MCP 条目要渲染进多个 IDE 的配置文件。
	Targets []Target `json:"targets"`
	// Content 是最终字节内容（已做变量替换与密钥内联）。
	Content string `json:"content"`
	// Secret 表示内容含密钥，代理必须以 0600 落盘（需求 2.7）。
	Secret bool `json:"secret"`
	// Mode 是可选的显式权限，0 表示按 Secret 推断。
	Mode uint32 `json:"mode,omitempty"`
	// Format 提示代理如何合并：whole 表示整文件覆盖，json_key 表示只改带 dec- 前缀的键。
	Format Format `json:"format"`
	// Key 在 json_key 格式下是要写入的键名，必须是 dec- 前缀。
	Key string `json:"key,omitempty"`
}

// Format 是落盘方式。
type Format string

const (
	// FormatWhole 表示整个文件由 Dec 拥有，直接覆盖。
	FormatWhole Format = "whole"
	// FormatJSONKey 表示文件里只管理一个键，其他键必须原样保留（需求 2.7）。
	FormatJSONKey Format = "json_key"
)

// Batch 是一次下发。
type Batch struct {
	// Revision 是 SSOT 的 revision，用于回执对账。
	Revision string `json:"revision"`
	Files    []File `json:"files"`
	Deletes  []File `json:"deletes"`
}

// ManifestEntry 是代理上报的一条本地状态。
type ManifestEntry struct {
	ID string `json:"id"`
	// Hash 是代理当场计算的伪内容哈希。代理不在本地保存下发清单（需求 5.2），
	// 所以每次对账都重新计算。
	Hash  string   `json:"hash"`
	Paths []string `json:"paths"`
	// Format 与 Key 与下发时一致，便于服务端发现“文件被外部改成了别的形状”。
	Format Format `json:"format"`
	Key    string `json:"key,omitempty"`
}

// SyncRequest 是对账请求。
type SyncRequest struct {
	// Revision 是代理最后一次成功应用的 revision，空表示首次。
	Revision string `json:"revision"`
	// Manifest 是代理当前的本地状态。
	Manifest []ManifestEntry `json:"manifest"`
	// 代理本地的项目登记，服务端据此校验路径是否属于已登记项目（需求 23.4）。
	Projects []LocalProject `json:"projects"`
	// WaitSeconds 是长轮询等待秒数（需求 4.2 的“立即推送”）。
	//
	// 传输是单向请求/响应，没有服务端主动推送的通道，所以用长轮询表达：
	// 服务端在没有差异时挂起这个请求，直到内容变更或超时。
	// 0 表示立即返回，代理首次连接时用它做全量对账。
	WaitSeconds int `json:"wait_seconds,omitempty"`
}

// LocalProject 是代理侧的项目登记投影。
type LocalProject struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Override 描述一次本地改动：Dec 往这个落点下发时覆盖了本地内容（需求 6.2 的 6.2 项）。
type Override struct {
	ID    string   `json:"id"`
	Hash  string   `json:"hash"`
	Paths []string `json:"paths"`
	// DetectedAt 由代理给出发现时间。
	DetectedAt time.Time `json:"detected_at"`
}

// SyncResponse 返回需要补齐的下发和已记录的结果。
type SyncResponse struct {
	Batch *Batch `json:"batch,omitempty"`
	// Accepted 表示本次上报的覆盖记录已被接受，代理可以清零本地待报队列。
	Accepted bool `json:"accepted"`
	// Overrides 是服务端在对账中直接检测到的本地改动（需求 6.2）。
	// 代理无需再自己判断，按此记入审计即可；追加字段，旧代理忽略。
	Overrides []Override `json:"overrides,omitempty"`
	// Alerts 是云端希望设备弹出的系统通知（需求 12.1 的第二条通道）。
	//
	// 挂在 sync 上而不是单独开端点：代理反正都在长轮询，多一个端点只是多一条
	// 需要鉴权和保活的路径。追加字段，旧代理忽略。
	Alerts []Alert `json:"alerts,omitempty"`
	// Checks 是本轮要核对的落点（需求 5.2）。
	//
	// 需求要求代理不在本机保存下发清单，期望状态只存在云端。所以由云端在每次
	// 响应里给出“这些条目的落点在这些绝对路径上，请算一下哈希报回来”，
	// 代理下一轮把这些哈希填进请求的 Manifest 即可。代理因此不需要任何本地清单，
	// 重启后也只需等一轮就能恢复对账。追加字段，旧代理想象成空即可。
	Checks []ManifestEntry `json:"checks,omitempty"`
}

// Alert 是一条下发给设备的告警。
type Alert struct {
	// ID 是稳定标识，代理据此去重，避免同一条通知弹两次。
	ID string `json:"id"`
	// Kind 是类别。
	Kind string `json:"kind"`
	// Title 是一句话标题，直接拿去弹系统通知。
	Title string `json:"title"`
	// Detail 是补充信息。
	Detail string `json:"detail,omitempty"`
	// Count 是合并后的次数。
	Count int `json:"count,omitempty"`
	// At 是最近一次发生时间。
	At time.Time `json:"at"`
}

// AckRequest 回报一个 batch 的落地结果。
type AckRequest struct {
	Revision string `json:"revision"`
	// Results 按文件 ID 汇总。
	Results   []AckResult `json:"results"`
	Overrides []Override  `json:"overrides,omitempty"`
}

// AckResult 是单个文件的结果。
type AckResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Hash 是落盘后重新计算的哈希，服务端用它更新期望状态。
	Hash string `json:"hash,omitempty"`
	// Paths 是实际写入的绝对路径，用于审计与吊销后的轮换清单（需求 18.11）。
	Paths []string `json:"paths,omitempty"`
}

// AckResponse 是空结构，保留字段以便未来追加。
type AckResponse struct {
	Revision string `json:"revision"`
}

// EnrollBeginRequest 是登记的第一步：提交 Bitwarden 凭据与设备公钥。
type EnrollBeginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp,omitempty"`
	// DevicePublicKey 是 PEM 编码的公钥，私钥永远不离开设备。
	DevicePublicKey string `json:"device_public_key"`
	DeviceName      string `json:"device_name"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
}

// EnrollBeginResponse 表示凭据核对通过，设备证书已签发。
type EnrollBeginResponse struct {
	DeviceID string `json:"device_id"`
	// Certificate 是 PEM 编码的设备证书，私钥仍在设备侧。
	Certificate string `json:"certificate"`
	// CA 是签发这张证书的服务端身份证书，代理可选地记录用于排障。
	CA string `json:"ca"`
	// CertNotAfter 供代理安排续签。
	CertNotAfter time.Time `json:"cert_not_after"`
	// IsFirst 表示这是初始化流程，本设备同时成为第一台“我的设备”。
	IsFirst bool `json:"is_first"`
}

// RecoverRequest 是恢复模式下的恢复请求，结构与登记相同，
// 区别在于服务端会先从 Bitwarden 取回根密钥写回本地文件，再签发证书。
type RecoverRequest = EnrollBeginRequest

// AssetPayload 是资产写入的条目内容。
type AssetPayload struct {
	Bundle string      `json:"bundle"`
	Items  []AssetItem `json:"items"`
	// Delete 是本次显式要删除的条目路径。
	Delete   []string `json:"delete,omitempty"`
	Manifest []byte   `json:"manifest,omitempty"`
	// BaseRevision 用于乐观并发控制，空表示允许覆盖。
	BaseRevision string `json:"base_revision,omitempty"`
	// Message 是提交说明，写进 commit。
	Message string `json:"message,omitempty"`
}

// AssetItem 是一条资产的原始内容。
type AssetItem struct {
	Path string `json:"path"`
	// Content 是文本内容。目录型资产（skill）由多个 AssetItem 组成，
	// 每一条都是一个文件，Path 以目录名开头。
	Content []byte `json:"content"`
	// Executable 标记需要保留可执行位。
	Executable bool `json:"executable,omitempty"`
}

// AssetResult 是写入结果。
type AssetResult struct {
	Bundle   string `json:"bundle"`
	Revision string `json:"revision"`
	Commit   string `json:"commit"`
	// Pushed 表示已触发下发，正常情况总是 true，因为写入是同步链路（需求 7.1）。
	Pushed bool `json:"pushed"`
}

// ReadAssetRequest 读取一个 bundle 的完整内容。
type ReadAssetRequest struct {
	Bundle string `json:"bundle"`
}

// AssetEntry 是 manifest 里的一条条目，附带给管理面看的可读信息。
type AssetEntry struct {
	Kind   string   `json:"kind"`
	Path   string   `json:"path"`
	Name   string   `json:"name"`
	Planes []string `json:"planes,omitempty"`
	Host   []string `json:"host,omitempty"`
}

// ReadAssetResponse 是一个 bundle 的完整内容与声明。//
// 内容是不带密钥的原文（`{{secret:...}}` 还是占位符）：管理面浏览资产时
// 不该顺手把密钥明文抄到浏览器里，真正要下发的内容由云端在渲染时内联。
type ReadAssetResponse struct {
	Bundle   string `json:"bundle"`
	Revision string `json:"revision"`
	// Manifest 是 manifest.yaml 的原文。
	//
	// 一并带回来是因为写入是“整体替换”语义：管理面保存一个文件时，必须把
	// manifest 原样写回，否则服务端会按文件树重新推断，planes 与 host 这些
	// 声明就被悄悄改掉了——改一个错别字顺手缩小了资产的下发范围，很难发现。
	Manifest []byte       `json:"manifest,omitempty"`
	Items    []AssetEntry `json:"items"`
	Files    []AssetItem  `json:"files"`
}

// OverridesRequest 查询本地改动覆盖记录。
//
// DeviceID 留空表示查当前设备自己：代理定期对账时就走这条路，
// 它不需要知道自己是谁，服务端从证书里看得出来。
type OverridesRequest struct {
	DeviceID string `json:"device_id,omitempty"`
}

// OverridesResponse 是本地改动覆盖记录。
type OverridesResponse struct {
	DeviceID  string     `json:"device_id"`
	Overrides []Override `json:"overrides"`
}

// SecretPayload 是密钥写入。
type SecretPayload struct {
	Name string `json:"name"`
	// Value 是明文。写入后服务端立刻加密，明文只存在于这一次请求的内存里。
	Value string `json:"value"`
	// Notes 是给人看的说明，不含明文。
	Notes string `json:"notes,omitempty"`
	// 渲染目标：哪些 bundle 的哪些条目引用它。空表示暂时不被引用。
	Refs []SecretRef `json:"refs,omitempty"`
	// BaseRevision 用于乐观并发控制。
	BaseRevision string `json:"base_revision,omitempty"`
	Message      string `json:"message,omitempty"`
}

// SecretRef 是一条引用关系。
type SecretRef struct {
	Bundle string `json:"bundle"`
	Item   string `json:"item"`
}

// SecretMeta 是密钥元数据，永不含明文。
type SecretMeta struct {
	Name      string      `json:"name"`
	Notes     string      `json:"notes,omitempty"`
	Refs      []SecretRef `json:"refs,omitempty"`
	UpdatedAt time.Time   `json:"updated_at"`
	// Devices 是曾经下发到哪些设备的计数，用于吊销后的轮换清单。
	Devices []string `json:"devices,omitempty"`
}

// ReadSecretRequest 请求明文。
type ReadSecretRequest struct {
	Name string `json:"name"`
	// Reason 是查看理由，会原样写进审计。
	//
	// 不强制填写：CLI 里临时排障不该被一个必填项拦住。但管理面会填，
	// 因为事后回看审计时，“为什么要看”比“谁看了”更难重建。
	Reason string `json:"reason,omitempty"`
}

// ReadSecretResponse 返回明文与理由。
type ReadSecretResponse struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Reason 回显实际记入审计的理由。
	Reason string `json:"reason"`
}

// DeviceInfo 是管理面看到的设备。
type DeviceInfo struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	OS              string     `json:"os"`
	Arch            string     `json:"arch"`
	Online          bool       `json:"online"`
	Revoked         bool       `json:"revoked"`
	RegisteredAt    time.Time  `json:"registered_at"`
	LastSeen        *time.Time `json:"last_seen,omitempty"`
	ReportedVersion string     `json:"reported_version,omitempty"`
	CertNotAfter    time.Time  `json:"cert_not_after"`
	UserBundles     []string   `json:"user_bundles,omitempty"`
	// Drift 是与云端期望状态的差异条数。
	Drift int `json:"drift"`
}

// StatusResponse 是服务状态的快照，管理面首屏就用它。
type StatusResponse struct {
	ServerVersion   string     `json:"server_version"`
	RequiredVersion string     `json:"required_version"`
	ProtocolVersion int        `json:"protocol_version"`
	Maintenance     bool       `json:"maintenance"`
	BootstrapOpen   bool       `json:"bootstrap_open"`
	RecoveryMode    bool       `json:"recovery_mode"`
	Revision        string     `json:"revision"`
	BackupCheckedAt *time.Time `json:"backup_checked_at,omitempty"`
	RootKeyID       string     `json:"root_key_id,omitempty"`
}
