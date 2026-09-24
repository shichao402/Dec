package agenttools

// 输入参数 struct 与旧 internal/mcp 一致：json / jsonschema 标签驱动 InputSchema。
// 描述不得匹配 google/jsonschema-go 的 ^[^\s]*= 前缀禁令。

type emptyParams struct{}

type connectParams struct {
	ID            string `json:"id,omitempty" jsonschema:"已保存连接 id；优先于 kind"`
	Kind          string `json:"kind,omitempty" jsonschema:"local | ssh | remote；无 id 时使用"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	SSHHost       string `json:"ssh_host,omitempty"`
	SSHUser       string `json:"ssh_user,omitempty"`
	TLS           bool   `json:"tls,omitempty"`
	TLSServerName string `json:"tls_server_name,omitempty"`
}

type registerManagedProjectParams struct {
	Root  string `json:"root" jsonschema:"项目根目录绝对路径"`
	Label string `json:"label,omitempty" jsonschema:"显示名，可空"`
}

type createLocalAssetParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global。留空默认 local；不支持 both。"`
	Project     string `json:"project" jsonschema:"vault 项目名"`
	Kind        string `json:"kind" jsonschema:"skill|rule|mcp|command 或私密类型 id"`
	Name        string `json:"name" jsonschema:"资产名"`
	Visibility  string `json:"visibility,omitempty" jsonschema:"public|private，默认 private"`
	AssetPlane  string `json:"asset_plane,omitempty" jsonschema:"象限平面 user|project；global 平面会强制 global"`
}

type statusParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local（本仓库）、global（本机）。留空默认 local；不支持 both。project/user 为旧别名。"`
}

type connectRepoParams struct {
	RepoURL string `json:"repo_url" jsonschema:"Dec Git 仓库 URL"`
}

type initProjectParams struct {
	ProjectRoot       string `json:"project_root" jsonschema:"要初始化的项目根目录"`
	ApplyVaultProject bool   `json:"apply_vault_project,omitempty" jsonschema:"若 vault 存在同名 projects/<name>.yaml 则自动应用其 bundle"`
}

type listAssetsParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

type setRequiresParams struct {
	ProjectRoot string            `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 时必填"`
	Requires    map[string]string `json:"requires" jsonschema:"订阅表：项目名 → 订阅版本。只能是 latest 或 v*，都从注册表安装。整表覆盖，空表示清空订阅。"`
	Plane       string            `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。"`
}

type listSubscriptionCandidatesParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。"`
}

type pullParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

type pushParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

type proposeUpstreamParams struct {
	ProjectRoot string `json:"project_root" jsonschema:"消费仓或草稿所在项目根"`
	OriginRepo  string `json:"origin_repo" jsonschema:"提供方源仓 owner/name"`
	Asset       string `json:"asset,omitempty" jsonschema:"资产名"`
	Title       string `json:"title,omitempty"`
	Body        string `json:"body,omitempty"`
	Diff        string `json:"diff" jsonschema:"unified diff，空则拒绝"`
	Mode        string `json:"mode,omitempty" jsonschema:"auto|pr|issue"`
	Branch      string `json:"branch,omitempty" jsonschema:"开 PR 时已推送的分支名"`
}

type previewPushParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

type listSecretsParams struct {
	ProjectRoot   string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 或 both 时必填"`
	IncludeRemote *bool  `json:"include_remote,omitempty" jsonschema:"是否检查 Bitwarden 远端存在性（默认 true，可能要求在 Console 解锁）"`
	Plane         string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

type listDeleteCandidatesParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

type deleteItemInput struct {
	Kind          string `json:"kind" jsonschema:"dec | secret | bundle"`
	Type          string `json:"type,omitempty"`
	Name          string `json:"name,omitempty"`
	Vault         string `json:"vault,omitempty"`
	SecretPath    string `json:"secret_path,omitempty" jsonschema:"secret：项目根相对落地路径，同时就是 Bitwarden Note 名"`
	SecretsBundle string `json:"secrets_bundle,omitempty" jsonschema:"secret：Bitwarden folder"`
	BundleName    string `json:"bundle_name,omitempty"`
	ProjectName   string `json:"project_name,omitempty" jsonschema:"项目名；bundle_name 为兼容字段"`
	Visibility    string `json:"visibility,omitempty" jsonschema:"项目资产象限：public|private"`
	AssetPlane    string `json:"asset_plane,omitempty" jsonschema:"项目资产象限：user|project"`
}

type deleteParams struct {
	ProjectRoot string            `json:"project_root,omitempty" jsonschema:"项目根；plane 为 local 时必填"`
	Items       []deleteItemInput `json:"items" jsonschema:"要删除的条目"`
	Confirmed   bool              `json:"confirmed" jsonschema:"必须为 true 才会执行删除"`
	Plane       string            `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。候选项须来自同平面的 dec_list_delete_candidates。"`
}

type provisionRemoteParams struct {
	Alias     string   `json:"alias" jsonschema:"受管设备别名；留空时使用 ssh_target"`
	SSHTarget string   `json:"ssh_target" jsonschema:"系统 ssh 目标：Host 别名、主机名、user@host，或 host:36000；凭据由 ~/.ssh/config / ssh-agent 提供"`
	Tags      []string `json:"tags,omitempty" jsonschema:"设备标签，仅用于登记分类"`
	Branch    string   `json:"branch,omitempty" jsonschema:"安装分支，留空默认 main"`
	Confirmed bool     `json:"confirmed" jsonschema:"首次置备会远程执行安装脚本，必须显式设为 true"`
}
