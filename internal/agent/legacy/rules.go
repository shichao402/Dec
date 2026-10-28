// Package legacy 认定并清理旧版 Dec 在本机留下的残留（.agent/legacy-cleanup）。
//
// 规则表随代理版本发布（需求 2.1）：清什么、为什么清都写在这里，
// 不从云端下发，也不读外部配置。规则以类别为粒度整体命中（需求 2.2），
// 不做逐文件甄别；防误删的手段是排除面加归档，而不是缩小命中范围。
//
// 新旧两代共用 dec- 前缀（云端下发的 MCP 键、SSH 密钥名），所以排除面
// 必须包含云端当前为本设备期望的条目（需求 2.3）。代理不在本地保存下发清单，
// 拿不到期望条目时扫描直接失败，不退化为只看名字（需求 2.3.1）。
package legacy

// Action 是规则命中后的处理方式。
type Action string

const (
	// ActionDelete 归档后整项删除。
	ActionDelete Action = "delete"
	// ActionStrip 用于共享文件：整份归档，只删其中的旧版条目。
	ActionStrip Action = "strip"
	// ActionList 只列出，不删除。用于无法归档的对象，以及仍可能在用的旧版凭据。
	ActionList Action = "list"
)

// Group 是规则在工具页上的分组。
type Group string

const (
	// GroupLegacy 是旧版残留，一次确认后清理。
	GroupLegacy Group = "legacy"
	// GroupCredential 是旧版留下、但仍可能在用的凭据（需求 2.6）。
	// 新系统还没有对应的落盘能力，删掉会让正在用的链路直接断掉，所以只列出。
	// SSH 与项目 .secrets 由新系统接管后转成普通规则（credential-landing 需求 7）；
	// 某个根尚未接管时，扫描仍把那里的文件按本组只列出。
	GroupCredential Group = "credential"
)

// Rule 是规则表的一行。
type Rule struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Action Action `json:"action"`
	Group  Group  `json:"group"`
}

// 规则编号与 .agent/legacy-cleanup/rules.md 一一对应。
const (
	RuleDecHome       = "H01"
	RuleDecsHome      = "H02"
	RuleShellRC       = "H03"
	RuleConsoleApp    = "H04"
	RuleConsoleData   = "H05"
	RuleOldBinaries   = "H06"
	RuleInternalHomes = "H07"
	RuleUserHostItems = "H08"
	RuleBrokenLinks   = "H09"
	RuleUserMCP       = "H10"
	RuleGitFragments  = "H11"
	RuleGCM           = "H12"
	RuleSSH           = "H13"
	RuleDecSecrets    = "H14"
	RuleSSHInclude    = "H15"

	RuleProjectDec      = "P01"
	RuleProjectInternal = "P02"
	RuleProjectHost     = "P03"
	RuleProjectMCP      = "P04"
	RuleProjectSecrets  = "P06"
)

// Rules 是完整规则表。
var Rules = []Rule{
	{RuleDecHome, "旧版本机状态（~/.dec 下的 config.yaml、repo.git、cache、secrets、bin 等）", ActionDelete, GroupLegacy},
	{RuleDecsHome, "更早的 ~/.decs 目录", ActionDelete, GroupLegacy},
	{RuleShellRC, "shell 配置中指向 ~/.dec/bin、~/.decs/bin 的 PATH 行", ActionStrip, GroupLegacy},
	{RuleConsoleApp, "旧版 dec-console.app", ActionDelete, GroupLegacy},
	{RuleConsoleData, "旧版 Console 的数据、缓存与偏好", ActionDelete, GroupLegacy},
	{RuleOldBinaries, "旧版二进制（dec、dec-server、dec-console 等）", ActionDelete, GroupLegacy},
	{RuleInternalHomes, "已移除宿主的用户目录（~/.claude-internal、~/.codex-internal）", ActionDelete, GroupLegacy},
	{RuleUserHostItems, "用户级宿主目录里旧版渲染的 dec-* skill、rule、command", ActionDelete, GroupLegacy},
	{RuleBrokenLinks, "宿主 skills、rules、commands 目录里的失效软链接", ActionDelete, GroupLegacy},
	{RuleUserMCP, "用户级 MCP 配置里的旧版 dec / dec-* 条目", ActionStrip, GroupLegacy},
	{RuleGitFragments, "旧版项目 Git 凭据片段（~/.config/dec/gitconfig.d 与 ~/.gitconfig 中的 includeIf）", ActionList, GroupCredential},
	{RuleGCM, "Git Credential Manager 中的旧版凭据（无法归档）", ActionList, GroupCredential},
	{RuleSSH, "不在云端期望里的 SSH 密钥与配置片段（~/.ssh/dec_*、config.d/dec*.conf）", ActionDelete, GroupLegacy},
	{RuleDecSecrets, "旧版本机 secret 落地（~/.dec/secrets）", ActionList, GroupCredential},
	{RuleSSHInclude, "~/.ssh/config 托管块之外的裸 Include config.d/dec*.conf 行", ActionStrip, GroupLegacy},

	{RuleProjectDec, "项目 .dec 下的旧版条目（config.yaml、vars.yaml、cache、overrides 等）", ActionDelete, GroupLegacy},
	{RuleProjectInternal, "项目里已移除宿主的目录（.claude-internal、.codex-internal）", ActionDelete, GroupLegacy},
	{RuleProjectHost, "项目宿主目录里旧版渲染的 dec-* 条目与失效软链接", ActionDelete, GroupLegacy},
	{RuleProjectMCP, "项目 MCP 配置里的旧版 dec-* 条目", ActionStrip, GroupLegacy},
	{RuleProjectSecrets, "项目 .secrets 下不在云端期望里的文件（.gitignore 除外）", ActionDelete, GroupLegacy},
}

// RuleByID 按编号取规则。
func RuleByID(id string) (Rule, bool) {
	for _, r := range Rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// oldUserHostDirs 是旧版渲染 skill、rule、command 的用户级宿主根（相对用户目录）。
//
// 这是旧版的布局，刻意不复用代理的宿主白名单：两者描述的是不同代的东西，
// 新代理以后增删宿主不应该改变“旧版写过哪里”。
var oldUserHostDirs = []string{
	".cursor",
	".claude",
	".codebuddy",
	".codex",
	".bg-agent/config-with-app",
}

// oldProjectHostDirs 是旧版的项目级宿主根（相对项目根）。
var oldProjectHostDirs = []string{
	".cursor",
	".claude",
	".codebuddy",
	".codex",
	".with",
}

// assetDirs 是宿主根下旧版写入的资产目录。
var assetDirs = []string{"skills", "rules", "commands"}

// oldUserMCPFiles 是旧版写过 MCP 条目的用户级配置（相对用户目录）。
var oldUserMCPFiles = []string{
	".cursor/mcp.json",
	".claude/mcp.json",
	".claude.json",
	".mcp.json",
	".codebuddy/mcp.json",
	".bg-agent/config-with-app/mcp_config.json",
	".codex/config.toml",
}

// oldProjectMCPFiles 是旧版写过 MCP 条目的项目级配置（相对项目根）。
var oldProjectMCPFiles = []string{
	".cursor/mcp.json",
	".claude/mcp.json",
	".mcp.json",
	".with/mcp_config.json",
	".codex/config.toml",
	".codex/mcp.json",
}

// oldBinaryNames 是旧版装进 ~/.local/bin 的可执行文件名。
var oldBinaryNames = []string{
	"dec", "dec-server", "dec-console", "dec-exec", "dec-mcp", "dec-host-setup", "dec-registry",
}

// shellRCFiles 是旧版安装脚本会追加 PATH 的 shell 配置（相对用户目录）。
var shellRCFiles = []string{".zshrc", ".bashrc", ".bash_profile", ".profile"}
