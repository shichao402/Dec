package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shichao402/Dec/internal/proto"
)

// MCP 入口（需求 1）。
//
// 代理在本地完成 MCP 握手，工具清单也定义在本地，但每一次调用都原样转发
// 给云端：代理不实现业务逻辑，只负责把 MCP 的方法映射成协议方法，
// 并把来源固定为 mcp，由服务端再做一次权限判定（需求 1.6）。
//
// 本地清单只是第一道限制，不是安全边界。把某个工具从清单里去掉能减少
// 误用，但真正的拦截在服务端：读明文、设备管理、回滚、轮换根密钥这几类
// 方法对 mcp 来源一律拒绝（需求 1.5、已决策 12）。

// MCPServer 是挂在 /mcp 上的 MCP 服务。
type MCPServer struct {
	// Call 转发一次协议调用，来源由本包固定为 mcp。
	Call func(ctx context.Context, m proto.Method, payload json.RawMessage) (json.RawMessage, error)
	// Status 返回本机代理状态快照，用于在执行前判断是否可调用云端。
	Status func() Status
	// ServerName 与 ServerVersion 出现在 initialize 的 serverInfo 里。
	ServerName    string
	ServerVersion string
}

// NewMCPServer 构造 MCP 服务。
func NewMCPServer(opts MCPServer) *mcp.Server {
	name := opts.ServerName
	if name == "" {
		name = "dec"
	}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    name,
		Version: opts.ServerVersion,
	}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	registerTools(srv, opts)
	return srv
}

// NewMCPHandler 构造 /mcp 的 HTTP 处理器（需求 1.1）。
//
// 用有状态的 Streamable HTTP 而不是无状态：MCP 的会话里存着客户端的
// 协议版本与能力协商结果，无状态模式每次请求都会退回默认值，
// 客户端会看到与 initialize 不一致的行为。
func NewMCPHandler(opts MCPServer) http.Handler {
	srv := NewMCPServer(opts)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		// JSONResponse 让响应是 application/json 而不是 SSE 流。
		// 本服务没有服务端主动推送的通知，纯请求-响应用 JSON 更省事：
		// curl 与大多数客户端的调试面板都能直接看到内容。
		JSONResponse: true,
	})
}

// instructions 是 initialize 时给客户端模型的总体说明。
//
// 这里写清边界，比逐个工具重复一遍更有效：模型读一次就知道自己不负责
// 哪些事，不会试着去调一个不存在的工具。
const instructions = `Dec 是用户的个人资产与密钥仓库。你通过这里的工具读写云端 SSOT 里的资产（skill、rule、MCP 配置）与密钥元数据。

边界：
- 你读不到密钥明文，也看不到设备表、回滚与根密钥轮换相关的任何操作。这些不是被隐藏，而是被禁止。
- 修改已有文件的内容，一律用 dec_edit_asset_file：只给文件路径和替换片段，其余文件与 manifest 由服务端原样保留。落点副本开头的「由 Dec 管理」说明行会写明该填的 bundle 与 path，照抄即可。
- 不要直接编辑本机上的副本（~/.claude/skills 等目录下的文件），也不要编辑任何看起来像源的本地目录：副本会在下次对账被覆盖，本地目录不会被 Dec 读取，两种改法都不会生效。
- dec_write_asset 是整体替换语义，只用于新建 bundle、增删文件或改 manifest 这类结构性改动：一个 bundle 提交后，它的内容就等于你这次给出的条目集合，少给的条目会被删除。
- 每次写入都会立即推送到该用户所有在线设备，并进入 git 审计与历史。写完不能悄悄撤回，只能再写一次。
- 删除单个条目请用 dec_delete_asset_item，它只按路径删一条，不会波及其他条目。
- 本地工具清单只是第一道限制，服务端会再判定一次权限。被拒绝时错误信息里会写明原因。`

// registerTools 注册最小工具集（需求 1.4）。
func registerTools(srv *mcp.Server, opts MCPServer) {
	// 状态
	addTool(srv, toolStatus(opts), opts)
	// 资产
	addTool(srv, toolListAssets(), opts)
	addTool(srv, toolReadAsset(), opts)
	addTool(srv, toolEditAssetFile(), opts)
	addTool(srv, toolWriteAsset(), opts)
	addTool(srv, toolDeleteAssetItem(), opts)
	addTool(srv, toolDeleteBundle(), opts)
	// 密钥（只有元数据，没有明文）
	addTool(srv, toolListSecrets(), opts)
	addTool(srv, toolPutSecret(), opts)
	addTool(srv, toolDeleteSecret(), opts)
	// 历史
	addTool(srv, toolHistory(), opts)
	// 项目登记与订阅
	addTool(srv, toolListProjects(), opts)
	addTool(srv, toolPutProject(), opts)
	addTool(srv, toolDeleteProject(), opts)
	addTool(srv, toolListSubscriptions(), opts)
	addTool(srv, toolSetSubscriptions(), opts)
}

// toolSpec 是一个工具的本地定义。
type toolSpec struct {
	tool *mcp.Tool
	// method 是转发到云端时用的协议方法。
	method proto.Method
	// build 把 MCP 的原始参数整理成云端要的载荷。
	build func(args json.RawMessage) (json.RawMessage, error)
	// post 可选，把云端返回整理成给模型看的形式。不设则原样返回。
	post func(out json.RawMessage) (json.RawMessage, error)
}

// addTool 把一个工具定义挂到服务上。
//
// 统一走这里而不是各写各的 handler：方法映射、来源固定、错误映射这些
// 约束只写一遍，新增工具时不可能漏掉其中任何一条。
func addTool(srv *mcp.Server, spec toolSpec, opts MCPServer) {
	srv.AddTool(spec.tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if opts.Call == nil {
			return toolError("代理没有配置云端调用通道，无法执行；请先执行 dec-agent configure --url https://... 并登记设备"), nil
		}
		if err := checkReady(opts); err != nil {
			return toolError(err.Error()), nil
		}
		payload, err := spec.build(req.Params.Arguments)
		if err != nil {
			return toolError(fmt.Sprintf("参数不正确：%v", err)), nil
		}
		out, err := opts.Call(ctx, spec.method, payload)
		if err != nil {
			return toolError(explainCallError(err)), nil
		}
		if spec.post != nil {
			if out, err = spec.post(out); err != nil {
				return toolError(fmt.Sprintf("整理云端返回失败：%v", err)), nil
			}
		}
		return toolResult(out), nil
	})
}

// checkReady 在转发前检查本机代理是否具备调用条件（需求 1.8）。
//
// 这里的判断与服务端的回执是两层：本机能立刻说清的原因（没登记、版本不对齐、
// 服务端维护中）不该先发一趟网络请求再说；说不清的交给服务端。
func checkReady(opts MCPServer) error {
	if opts.Status == nil {
		return nil
	}
	st := opts.Status()
	if st.DeviceID == "" {
		return errors.New("这台设备还没有登记：先执行 dec-agent enroll（或打开 http://127.0.0.1:43679/admin 登记），再重试")
	}
	// 先判对齐再判维护：版本不对齐时代理按心跳（10 分钟）才再握手，
	// 期间维护标志可能停在部署那一刻的 true，先报它会把“该升级代理”误说成“等部署”。
	if !st.Aligned {
		required := st.RequiredVer
		if required == "" {
			required = "服务端要求的新版本"
		}
		return fmt.Errorf("代理版本与服务端要求不一致：本机 %s，服务端要求 %s。请先升级代理（管理面会给出下载地址）", st.Version, required)
	}
	if st.Maintenance {
		return errors.New("服务端正在部署（维护中），此刻的写入不会被接受；稍后重试，或用 dec_status 确认")
	}
	return nil
}

// explainCallError 把转发失败翻译成能定位原因的话（需求 1.8）。
//
// 不做「返回空结果」这种退化：调用方拿到空结果会以为云端真没有数据，
// 而实际是这次调用根本没成功。
func explainCallError(err error) string {
	switch {
	case errors.Is(err, ErrNotConfigured):
		return "还没有配置服务地址：先执行 dec-agent configure --url https://..."
	case errors.Is(err, ErrMaintenance):
		return "服务端维护中，拒绝本次调用：" + err.Error()
	case errors.Is(err, ErrNotAligned):
		return "代理版本与服务端要求不一致，本次调用未被接受：" + err.Error()
	case errors.Is(err, ErrIdentityRejected):
		return "设备身份被服务端拒绝（可能已被吊销或证书过期），请重新登记：" + err.Error()
	}
	if code := CodeOf(err); code != "" {
		return fmt.Sprintf("云端拒绝（%s）：%s", code, err.Error())
	}
	return "云端调用失败：" + err.Error()
}

// toolResult 把云端返回的 JSON 变成 MCP 工具结果。
//
// 内容同时给 text 与 structured：不同客户端取用其中之一，缺一个就会出现
// 「Claude 能看见、Cursor 看不见」这类差别。
func toolResult(raw json.RawMessage) *mcp.CallToolResult {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var structured any
	if err := json.Unmarshal(raw, &structured); err != nil {
		structured = map[string]any{"raw": string(raw)}
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: structured,
	}
}

// toolError 构造一个工具级错误结果。
//
// 用 IsError 而不是协议错误：协议错误会让客户端认为服务器坏了，
// 而这里大多数情况（没登记、版本不对齐）是使用者可以自己处理的。
func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// objectSchema 是构造输入 schema 的小工具，避免每个工具都写一大段字面量。
func objectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// strProp 是一个字符串参数。
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// boolProp 是一个布尔参数。
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// intProp 是一个整数参数。
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// arrProp 是一个数组参数。
func arrProp(desc string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": items}
}

// objProp 是一个对象数组参数。
func objProp(desc string, props map[string]any, required ...string) map[string]any {
	item := objectSchema(props, required...)
	return arrProp(desc, item)
}

// 以下每个 toolXxx 返回一个工具定义。

func toolStatus(opts MCPServer) toolSpec {
	return toolSpec{
		// 这里转发 hello 而不是 status：status 含根密钥 ID、初始化入口开关这类
		// 服务端内部信息，服务端对 mcp 来源一律拒绝（已定决策 12）。hello 返回
		// 的是同一批对外可见的状态——服务端版本、要求版本、协议版本、维护中、
		// 是否对齐——而且它本来就允许带证书调用，不需要额外开放权限。
		method: proto.MethodHello,
		tool: &mcp.Tool{
			Name: "dec_status",
			Description: "查询服务端对外状态：服务端版本、要求的代理版本、协议版本、是否维护中、" +
				"本机代理是否已与服务端对齐、本机登记的设备 ID。只读，无副作用。" +
				"写入前先用它确认服务端不在维护中。",
			InputSchema: objectSchema(map[string]any{}),
		},
		build: func(json.RawMessage) (json.RawMessage, error) {
			// 代理版本取自本机，不接受调用方指定：aligned 是「本机与服务端是否对齐」
			// 的结论，让调用方自己填版本等于让它给自己打分。
			ver := ""
			if opts.Status != nil {
				ver = opts.Status().Version
			}
			return json.Marshal(proto.HelloRequest{AgentVersion: ver})
		},
	}
}

func toolListAssets() toolSpec {
	return toolSpec{
		method: proto.MethodListAssets,
		tool: &mcp.Tool{
			Name: "dec_list_assets",
			Description: "列出云端所有资产 bundle 及其条目（kind、path、name、下发平面与宿主）。" +
				"只读，不返回文件正文。想看某个 bundle 的完整内容用 dec_read_asset。",
			InputSchema: objectSchema(map[string]any{}),
		},
		build: emptyPayload,
	}
}

func toolReadAsset() toolSpec {
	return toolSpec{
		method: proto.MethodReadAsset,
		tool: &mcp.Tool{
			Name: "dec_read_asset",
			Description: "读取一个资产 bundle 的完整内容：manifest 原文、条目声明与全部文件正文。" +
				"返回的正文里密钥仍然是 {{secret:...}} 占位符，不会出现明文。" +
				"manifest 与文本文件直接返回原文；二进制文件的 content 是 base64，并带 base64=true，" +
				"与 dec_write_asset 的入参格式一致，可以原样回传。只读，无副作用。",
			InputSchema: objectSchema(map[string]any{
				"bundle": strProp("bundle 名称，例如 my-skills"),
			}, "bundle"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.ReadAssetRequest{})
		},
		post: textReadAsset,
	}
}

// textReadAsset 把 read_asset 的返回整理成与 dec_write_asset 入参对称的形式。
//
// 云端协议里 manifest 与文件内容是 []byte，JSON 里是 base64。管理面前端会
// 自己解码，但模型不会：它照「manifest 原样回传」把 base64 串写回去，
// 结果是 manifest 解析失败或被重新推断，下发范围被悄悄改掉。这里在 MCP
// 层解码成原文，只有真正的二进制才保留 base64 并显式标注。
func textReadAsset(out json.RawMessage) (json.RawMessage, error) {
	var in proto.ReadAssetResponse
	if err := json.Unmarshal(out, &in); err != nil {
		return nil, err
	}
	type file struct {
		Path       string `json:"path"`
		Content    string `json:"content"`
		Executable bool   `json:"executable,omitempty"`
		Base64     bool   `json:"base64,omitempty"`
	}
	resp := struct {
		Bundle   string             `json:"bundle"`
		Revision string             `json:"revision"`
		Manifest string             `json:"manifest,omitempty"`
		Items    []proto.AssetEntry `json:"items"`
		Files    []file             `json:"files"`
	}{
		Bundle:   in.Bundle,
		Revision: in.Revision,
		Manifest: string(in.Manifest),
		Items:    in.Items,
		Files:    make([]file, 0, len(in.Files)),
	}
	for _, f := range in.Files {
		item := file{Path: f.Path, Executable: f.Executable}
		if utf8.Valid(f.Content) {
			item.Content = string(f.Content)
		} else {
			item.Content = base64.StdEncoding.EncodeToString(f.Content)
			item.Base64 = true
		}
		resp.Files = append(resp.Files, item)
	}
	return json.Marshal(resp)
}

func toolEditAssetFile() toolSpec {
	return toolSpec{
		method: proto.MethodEditAssetFile,
		tool: &mcp.Tool{
			Name: "dec_edit_asset_file",
			Description: "修改云端某个 bundle 里一个已存在文件的一段文本，这是修改 skill、rule 等资产内容的首选方式。" +
				"副作用：立即提交到云端 git 并推送给所有在线设备，进入审计与历史。" +
				"只需给出 bundle、文件路径、要替换的原文和新文本；其余文件与 manifest 由服务端原样保留，" +
				"不会因为少传而删掉任何东西。old_string 必须在文件里恰好出现一次，" +
				"出现 0 次或多次都会被拒绝，此时先用 dec_read_asset 读出原文，带上更多上下文再试。" +
				"落点副本开头的「由 Dec 管理」说明行里写着该文件对应的 bundle 与 path。" +
				"不能新增文件、不能改 manifest.yaml，这些结构性改动用 dec_write_asset。",
			InputSchema: objectSchema(map[string]any{
				"bundle":     strProp("bundle 名称，例如 relkit"),
				"path":       strProp("相对 bundle 根的文件路径，例如 skills/relkit-ops/SKILL.md（与说明行里的 path 相同）"),
				"old_string": strProp("要被替换的原文，必须逐字一致且在文件中只出现一次"),
				"new_string": strProp("替换后的文本；为空表示删除这段"),
				"base_revision": strProp("期望的当前 revision，用于乐观并发控制：与服务端不一致时拒绝写入。" +
					"留空表示允许覆盖"),
				"message": strProp("提交说明，写进 git commit，例如「修正 relkit-ops 的升级步骤」"),
			}, "bundle", "path", "old_string", "new_string"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.EditAssetFileRequest{})
		},
	}
}

func toolWriteAsset() toolSpec {
	return toolSpec{
		method: proto.MethodWriteAsset,
		tool: &mcp.Tool{
			Name: "dec_write_asset",
			Description: "整体替换一个资产 bundle 的内容。副作用：写入立即提交到云端 git 并推送给该用户所有在线设备，" +
				"同时进入审计与历史，写入后不能悄悄撤回。语义是整体替换——items 里没出现的旧条目会被删除，" +
				"只改已有文件的内容时不要用它，改用 dec_edit_asset_file。" +
				"用它时要么先 dec_read_asset 拿全量再改，要么明确知道自己在缩减这个 bundle。" +
				"manifest 建议原样回传 dec_read_asset 拿到的那份，否则服务端会按文件树重新推断 plane 与 host 声明。" +
				"只想删某一条时改用 dec_delete_asset_item，它不会波及其他条目。",
			InputSchema: objectSchema(map[string]any{
				"bundle": strProp("bundle 名称，例如 my-skills"),
				"items": objProp("这个 bundle 下的全部文件，不是增量补丁。少传一个就等于删掉它。",
					map[string]any{
						"path":       strProp("相对 bundle 根的文件路径，例如 skills/foo/SKILL.md"),
						"content":    strProp("文件全文。文本内容直传；需要传二进制时用 base64 并置 base64=true"),
						"executable": boolProp("是否保留可执行位，默认 false"),
						"base64":     boolProp("content 是否为 base64 编码，默认 false"),
					}, "path", "content"),
				"manifest": strProp("manifest.yaml 原文。留空时服务端按 items 推断，会丢掉原有的 plane/host 声明"),
				"base_revision": strProp("期望的当前 revision，用于乐观并发控制：与服务端不一致时写入被拒绝并返回最新 revision。" +
					"留空表示允许覆盖，可能覆盖别人刚做的改动"),
				"message": strProp("提交说明，写进 git commit，例如「补充 my-skills 的 release 流程」"),
			}, "bundle", "items"),
		},
		build: buildWriteAsset,
	}
}

func toolDeleteAssetItem() toolSpec {
	return toolSpec{
		method: proto.MethodDeleteAsset,
		tool: &mcp.Tool{
			Name: "dec_delete_asset_item",
			Description: "按路径删除一个资产 bundle 里的单个条目。副作用：立即提交并推送给所有在线设备，进入审计与历史。" +
				"只影响路径完全匹配的那一条，其余条目与文件保持不变（不同于 dec_write_asset 的整体替换）。" +
				"路径不存在时返回 not_found，不会静默成功。",
			InputSchema: objectSchema(map[string]any{
				"bundle":        strProp("bundle 名称"),
				"path":          strProp("要删除的条目路径，必须与 manifest 里的 path 完全一致，例如 skills/foo"),
				"base_revision": strProp("期望的当前 revision，用于乐观并发控制，留空表示允许覆盖"),
				"message":       strProp("提交说明，写进 git commit"),
			}, "bundle", "path"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.DeleteAssetRequest{})
		},
	}
}

func toolDeleteBundle() toolSpec {
	return toolSpec{
		method: proto.MethodDeleteBundle,
		tool: &mcp.Tool{
			Name: "dec_delete_bundle",
			Description: "删除整个资产 bundle（目录与 manifest 一起删除，列表里随即消失）。" +
				"副作用：立即提交并推送给该用户所有在线设备，进入审计与历史；订阅了这个 bundle 的设备" +
				"会在下一次对账里收到删除指令，落点被清理。bundle 不存在时同样返回成功，便于重复清理。" +
				"只删一个条目用 dec_delete_asset_item；把 bundle 缩减到只剩几条用 dec_write_asset。" +
				"这是不可逆的：删空之后要恢复只能靠历史里的 commit。",
			InputSchema: objectSchema(map[string]any{
				"bundle":        strProp("要删除的 bundle 名称，例如 my-skills"),
				"base_revision": strProp("期望的当前 revision，用于乐观并发控制，留空表示允许覆盖"),
				"message":       strProp("提交说明，写进 git commit"),
			}, "bundle"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.DeleteBundleRequest{})
		},
	}
}

func toolListSecrets() toolSpec {
	return toolSpec{
		method: proto.MethodListSecrets,
		tool: &mcp.Tool{
			Name: "dec_list_secrets",
			Description: "列出密钥的元数据：名称、说明、被哪些 bundle 条目引用、最近更新时间。" +
				"永远不返回明文——明文读取对 MCP 来源是被服务端禁止的，不是这里过滤掉的。只读，无副作用。",
			InputSchema: objectSchema(map[string]any{}),
		},
		build: emptyPayload,
	}
}

func toolPutSecret() toolSpec {
	return toolSpec{
		method: proto.MethodPutSecret,
		tool: &mcp.Tool{
			Name: "dec_put_secret",
			Description: "写入或更新一个密钥的值。副作用：明文只在这一次请求的内存里存在，服务端立即加密后落进 SSOT，" +
				"并进入审计与历史；随后推送给所有在线设备。写入后无法读回明文，只能覆盖。" +
				"如果只是改说明而不改值，请改用其它方式，不要用占位值覆盖真实密钥。",
			InputSchema: objectSchema(map[string]any{
				"name":  strProp("密钥名称，例如 my-skills/github-token"),
				"value": strProp("明文值。写入后不可读回"),
				"notes": strProp("给人看的说明，不要包含明文"),
				"refs": objProp("这条密钥被哪些 bundle 条目引用，用于下发时替换 {{secret:...}} 占位符。留空表示暂不被引用。",
					map[string]any{
						"bundle": strProp("bundle 名称"),
						"item":   strProp("条目路径"),
					}, "bundle", "item"),
				"base_revision": strProp("期望的当前 revision，用于乐观并发控制，留空表示允许覆盖"),
				"message":       strProp("提交说明，写进 git commit"),
			}, "name", "value"),
		},
		build: buildPutSecret,
	}
}

func toolDeleteSecret() toolSpec {
	return toolSpec{
		method: proto.MethodDeleteSecret,
		tool: &mcp.Tool{
			Name: "dec_delete_secret",
			Description: "删除一个密钥。副作用：立即提交并推送给所有在线设备，进入审计与历史；引用它的资产会失去这个值。" +
				"默认拒绝删除仍被引用的密钥并列出引用方，确认要删时置 force=true。",
			InputSchema: objectSchema(map[string]any{
				"name":    strProp("要删除的密钥名称"),
				"force":   boolProp("即使仍被引用也删除。默认 false：被引用时拒绝并列出引用方"),
				"message": strProp("提交说明，写进 git commit"),
			}, "name"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.SecretDeleteRequest{})
		},
	}
}

func toolHistory() toolSpec {
	return toolSpec{
		method: proto.MethodHistory,
		tool: &mcp.Tool{
			Name: "dec_history",
			Description: "查询资产或密钥的改动历史：commit、说明、作者、来源与时间。" +
				"只读，无副作用。注意这里只读历史，回滚对 MCP 来源是被禁止的操作。",
			InputSchema: objectSchema(map[string]any{
				"kind":  strProp("查询对象类型：asset 或 secret"),
				"id":    strProp("对象标识：kind=asset 时是 bundle 名称，kind=secret 时是密钥名称"),
				"limit": intProp("最多返回几条，默认由服务端决定"),
			}, "kind", "id"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.HistoryRequest{})
		},
	}
}

func toolListProjects() toolSpec {
	return toolSpec{
		method: proto.MethodProjects,
		tool: &mcp.Tool{
			Name: "dec_list_projects",
			Description: "列出已登记的项目：ID、所属设备、名称、路径、启用的 bundle 与登记时间。" +
				"项目登记决定哪一个项目消费哪些 bundle。只读，无副作用。",
			InputSchema: objectSchema(map[string]any{}),
		},
		build: emptyPayload,
	}
}

func toolPutProject() toolSpec {
	return toolSpec{
		method: proto.MethodPutProject,
		tool: &mcp.Tool{
			Name: "dec_put_project",
			Description: "新增或修改一个项目登记。副作用：立即写入云端并进入审计与历史，" +
				"改动会影响该项目的下发范围（bundles 列表就是这个项目消费的资产集合）。" +
				"device_id 必填：项目属于某台设备，不指定设备就无法确定落点。",
			InputSchema: objectSchema(map[string]any{
				"id":        strProp("项目 ID。留空表示新增，填写表示修改已有项目"),
				"device_id": strProp("项目所属设备 ID，可从 dec_list_projects 或 dec_status 得到"),
				"name":      strProp("项目名称，小写 kebab-case"),
				"path":      strProp("项目在本机的绝对路径"),
				"bundles":   arrProp("这个项目消费的 bundle 名称列表，整体替换", map[string]any{"type": "string"}),
			}, "device_id", "name", "path"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.PutProjectRequest{})
		},
	}
}

func toolDeleteProject() toolSpec {
	return toolSpec{
		method: proto.MethodDeleteProject,
		tool: &mcp.Tool{
			Name: "dec_delete_project",
			Description: "删除一个项目登记。副作用：立即提交并进入审计与历史；" +
				"删除后该项目不再接收任何下发，但本机已落地的文件不会被自动清理。",
			InputSchema: objectSchema(map[string]any{
				"id": strProp("要删除的项目 ID"),
			}, "id"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.DeleteProjectRequest{})
		},
	}
}

func toolListSubscriptions() toolSpec {
	return toolSpec{
		method: proto.MethodProjects,
		tool: &mcp.Tool{
			Name: "dec_list_subscriptions",
			Description: "查看当前设备消费了哪些 bundle（用户平面订阅）。" +
				"返回值与 dec_list_projects 相同：订阅是按设备记录的，项目登记只是它的消费方。只读，无副作用。",
			InputSchema: objectSchema(map[string]any{}),
		},
		build: emptyPayload,
	}
}

func toolSetSubscriptions() toolSpec {
	return toolSpec{
		method: proto.MethodSetSubscriptions,
		tool: &mcp.Tool{
			Name: "dec_set_subscriptions",
			Description: "整体替换某台设备的用户平面订阅。副作用：立即提交并推送给该设备，" +
				"订阅减少时云端不再下发被移除的 bundle，但本机已落地的文件不会被自动清理。" +
				"语义是整体替换：没列出来的 bundle 等于退订。",
			InputSchema: objectSchema(map[string]any{
				"device_id": strProp("要修改订阅的设备 ID"),
				"bundles":   arrProp("订阅的 bundle 名称全集，整体替换；空数组表示清空订阅", map[string]any{"type": "string"}),
			}, "device_id", "bundles"),
		},
		build: func(args json.RawMessage) (json.RawMessage, error) {
			return passThrough(args, &proto.SetSubscriptionsRequest{})
		},
	}
}

// emptyPayload 用于无参数工具。
func emptyPayload(json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

// passThrough 校验参数能解进目标结构体，然后原样转发。
//
// 转发的是调用方原始 JSON 而不是重新编码的结构体：MCP 客户端多给的字段
// 应当被如实带到服务端（服务端才是权威的解码方），本地重新编码会悄悄丢掉它们。
func passThrough(args json.RawMessage, target any) (json.RawMessage, error) {
	if len(args) == 0 || string(args) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(args, target); err != nil {
		return nil, err
	}
	return args, nil
}

// buildWriteAsset 整理写入载荷。
//
// 单独写一个是因为 content 需要支持 base64 解码：AssetItem.Content 是 []byte，
// 直接 json.Unmarshal 会把普通字符串当成 base64 解析，文本内容会静默损坏。
func buildWriteAsset(args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Bundle       string `json:"bundle"`
		Manifest     string `json:"manifest,omitempty"`
		BaseRevision string `json:"base_revision,omitempty"`
		Message      string `json:"message,omitempty"`
		Items        []struct {
			Path       string `json:"path"`
			Content    string `json:"content"`
			Executable bool   `json:"executable,omitempty"`
			Base64     bool   `json:"base64,omitempty"`
		} `json:"items"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Bundle) == "" {
		return nil, errors.New("bundle 不能为空")
	}

	payload := proto.AssetPayload{
		Bundle:       in.Bundle,
		Manifest:     []byte(in.Manifest),
		BaseRevision: in.BaseRevision,
		Message:      in.Message,
	}
	payload.Items = make([]proto.AssetItem, 0, len(in.Items))
	for _, it := range in.Items {
		content, err := decodeItemContent(it.Content, it.Base64)
		if err != nil {
			return nil, fmt.Errorf("条目 %s 的 content 无法解码: %w", it.Path, err)
		}
		payload.Items = append(payload.Items, proto.AssetItem{
			Path:       it.Path,
			Content:    content,
			Executable: it.Executable,
		})
	}
	return json.Marshal(payload)
}

// decodeItemContent 把条目正文还原成字节。
//
// base64=false 时正文就是纯文本，必须按原样取字节：proto.AssetItem.Content
// 是 []byte，交给 json 解码会把普通字符串当 base64 解析，文本会静默损坏。
func decodeItemContent(content string, isBase64 bool) ([]byte, error) {
	if !isBase64 {
		return []byte(content), nil
	}
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// buildPutSecret 整理密钥写入载荷。
func buildPutSecret(args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Name         string            `json:"name"`
		Value        string            `json:"value"`
		Notes        string            `json:"notes,omitempty"`
		Refs         []proto.SecretRef `json:"refs,omitempty"`
		BaseRevision string            `json:"base_revision,omitempty"`
		Message      string            `json:"message,omitempty"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("name 不能为空")
	}
	return json.Marshal(proto.SecretPayload{
		Name:         in.Name,
		Value:        in.Value,
		Notes:        in.Notes,
		Refs:         in.Refs,
		BaseRevision: in.BaseRevision,
		Message:      in.Message,
	})
}
