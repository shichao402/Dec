package agenttools

import (
	"fmt"
	"sync"
)

type toolDef struct {
	Name        string
	Description string
	Owner       string
	Sample      any // 零值输入，用于推导 schema
}

var (
	toolsOnce sync.Once
	allTools  []toolDef
	toolIndex map[string]toolDef
)

func catalog() []toolDef {
	toolsOnce.Do(func() {
		allTools = []toolDef{
			{
				Name:        "dec_console_status",
				Description: "查看本机 dec-server（版本、是否已解锁）。MCP 只打本机，不反映 Console 当前的远端连接。Console 自更新请由用户打开 Console 的更新面板。",
				Owner:       OwnerConsole,
				Sample:      emptyParams{},
			},
			{
				Name:        "dec_list_connections",
				Description: "已不再切换 Agent 目标。本机 MCP 只操作本机 dec-server；远端连接请用 Console 的连接页。",
				Owner:       OwnerConsole,
				Sample:      emptyParams{},
			},
			{
				Name:        "dec_connect",
				Description: "已不再切换 Agent 目标。本机 MCP 只操作本机 dec-server；远端连接请用 Console 的连接页。",
				Owner:       OwnerConsole,
				Sample:      connectParams{},
			},
			{
				Name:        "dec_list_managed_projects",
				Description: "列出当前目标设备上已登记的受管项目（路径、标签、是否已初始化）。",
				Owner:       OwnerServer,
				Sample:      emptyParams{},
			},
			{
				Name:        "dec_list_managed_devices",
				Description: "列出当前目标上已登记的受管 SSH 设备（只读）。",
				Owner:       OwnerServer,
				Sample:      emptyParams{},
			},
			{
				Name:        "dec_register_managed_project",
				Description: "把本地目录登记为当前目标上的受管项目。",
				Owner:       OwnerServer,
				Sample:      registerManagedProjectParams{},
			},
			{
				Name:        "dec_create_local_asset",
				Description: "创建资产。当前工作区就是该项目的本仓时写入 DecAssets/ 并登记 provides；已经订阅该项目时写入覆写草稿；其余写入 cache。不写别人的源仓。",
				Owner:       OwnerServer,
				Sample:      createLocalAssetParams{},
			},
			{
				Name:        "dec_status",
				Description: "查看某平面的 Dec 状态（仓库连接、绑定项目、requires 与 public/private × global/local 四象限；plane=local|global）。不检查或安装 Console 更新；自更新请由用户打开 Console。",
				Owner:       OwnerServer,
				Sample:      statusParams{},
			},
			{
				Name:        "dec_connect_repo",
				Description: "连接 Dec 资产 Git 仓库（全局配置，写入目标设备 ~/.dec/config.yaml）",
				Owner:       OwnerServer,
				Sample:      connectRepoParams{},
			},
			{
				Name:        "dec_init_project",
				Description: "初始化指定项目并绑定本仓项目（需已连接仓库）",
				Owner:       OwnerServer,
				Sample:      initProjectParams{},
			},
			{
				Name:        "dec_list_assets",
				Description: "列出某平面已订阅的项目、其 depends_on 闭包与四象限成员（plane=local|global|both）。",
				Owner:       OwnerServer,
				Sample:      listAssetsParams{},
			},
			{
				Name:        "dec_set_requires",
				Description: "设置本平面订阅（唯一消费声明）：项目名 → latest 或 v*，整表写 config.yaml 的 requires。不接受 vault。",
				Owner:       OwnerServer,
				Sample:      setRequiresParams{},
			},
			{
				Name:        "dec_list_subscription_candidates",
				Description: "列出可订阅项目：注册表里已发布的项目，外加本工作区的本仓项目。带订阅版本、已装版本、可用版本和 origin_repo。",
				Owner:       OwnerServer,
				Sample:      listSubscriptionCandidatesParams{},
			},
			{
				Name:        "dec_pull",
				Description: "拉取并安装某平面的项目四象限资产与 private secrets（plane=local|global|both）。",
				Owner:       OwnerServer,
				Sample:      pullParams{},
			},
			{
				Name:        "dec_push",
				Description: "把某平面的个人 Git 改动与 secrets 推回私仓 / Bitwarden（plane=local|global|both）。官方资产禁止 push，请用 dec_propose_upstream。",
				Owner:       OwnerServer,
				Sample:      pushParams{},
			},
			{
				Name:        "dec_propose_upstream",
				Description: "把官方资产草稿以 PR 或 Issue 提交到提供方源仓（mode=auto|pr|issue）。不写 Dec registry。合入并打产品 v* 后由提供方 CI publish。",
				Owner:       OwnerServer,
				Sample:      proposeUpstreamParams{},
			},
			{
				Name:        "dec_preview_push",
				Description: "预览某平面 push 将涉及的 Dec 与 secrets 变更（plane=local|global|both，不写远端）。推之前先 preview 确认范围。",
				Owner:       OwnerServer,
				Sample:      previewPushParams{},
			},
			{
				Name:        "dec_list_secrets",
				Description: "列出某平面私密资产元数据（路径、本地/远端存在性、来源仓 origin_repo、仅密钥 identity_only、疑似孤儿 orphan；plane=local|global|both）。绝不返回 token/密钥/正文。",
				Owner:       OwnerServer,
				Sample:      listSecretsParams{},
			},
			{
				Name:        "dec_list_delete_candidates",
				Description: "列出某平面可删除的项目四象限资产与 private secrets；legacy bundle 仍以兼容节点出现。",
				Owner:       OwnerServer,
				Sample:      listDeleteCandidatesParams{},
			},
			{
				Name:        "dec_delete",
				Description: "删除选中的项目资产、private secrets 或 legacy bundle（需 confirmed=true）。",
				Owner:       OwnerServer,
				Sample:      deleteParams{},
			},
			{
				Name:        "dec_provision_remote",
				Description: "通过系统 SSH 向 Linux/macOS 远端置备 Dec、配置固定 loopback 监听并登记设备。首次置备是远程代码执行，必须 confirmed=true。",
				Owner:       OwnerServer,
				Sample:      provisionRemoteParams{},
			},
		}
		toolIndex = make(map[string]toolDef, len(allTools))
		for _, t := range allTools {
			toolIndex[t.Name] = t
		}
	})
	return allTools
}

func lookupTool(name string) (toolDef, error) {
	catalog()
	t, ok := toolIndex[name]
	if !ok {
		return toolDef{}, fmt.Errorf("未知 Agent 工具 %q", name)
	}
	return t, nil
}

// OwnerOf 返回工具归属；未知工具返回空串。
func OwnerOf(name string) string {
	t, err := lookupTool(name)
	if err != nil {
		return ""
	}
	return t.Owner
}

// ToolNames 返回全部工具名（按注册顺序）。
func ToolNames() []string {
	tools := catalog()
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}
