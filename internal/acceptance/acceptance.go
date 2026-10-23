// Package acceptance 是真实链路的验收客户端（需求 4.1）。
//
// 它与 internal/agent 的单元测试是两个层次的东西：单元测试用假的云端，
// 这里连的是本机运行中的代理和它背后的生产云端。因此它不做任何假设，
// 每一步都从网络上读回真实结果，并且哪怕中途失败也要把测试数据清掉。
//
// 为什么不是 `go test`：验收要跑在生产环境上，不能因为一次「顺手跑测试」
// 就动到真实资产。它必须是一个显式的、由 scripts/acceptance.sh 调起的动作。
package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shichao402/Dec/internal/agent"
)

// Options 是一次验收的参数。
type Options struct {
	// BaseURL 是本机代理的回环地址，例如 http://127.0.0.1:8787。
	BaseURL string
	// Bundle 是本次验收使用的临时 bundle 名。必须在开始时随机生成，
	// 避免与真实资产同名，也避免两次验收互相干扰。
	Bundle string
	// ProjectPath 是用于项目平面探针的本机临时目录。
	ProjectPath string
	// Log 输出进度。为空时写 stdout。
	Log io.Writer
	// Timeout 是单步等待下发落地的上限。
	Timeout time.Duration
	// Keep 为 true 时即使全部通过也保留测试数据，便于人工观察。
	Keep bool
}

// Step 是一项检查的结果。
type Step struct {
	Name   string
	Passed bool
	Detail string
}

// Result 是一次验收的汇总。
type Result struct {
	Steps []Step
	// Residue 是清理后仍然存在、需要人工处理的东西。
	Residue []string
}

// Failed 返回未通过的检查项数量。
func (r *Result) Failed() int {
	n := 0
	for _, s := range r.Steps {
		if !s.Passed {
			n++
		}
	}
	return n
}

// Run 执行整条验收链路。
//
// 顺序刻意与人工排查的顺序一致：先确认页面能打开（说明代理活着），
// 再看它有没有跟上服务端（aligned），再确认各 IDE 的条目在，
// 最后才通过 MCP 做写入与删除。前面的任何一项失败都说明后面做不了，
// 所以一旦失败就立刻返回，不做无意义的后续尝试。
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Log == nil {
		opts.Log = os.Stdout
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 90 * time.Second
	}
	res := &Result{}
	r := &runner{opts: opts, res: res}

	// 清理必须无条件执行：写入之后任何一步失败都会留下测试 bundle。
	defer func() {
		r.cleanup(ctx)
	}()

	if err := r.run(ctx); err != nil {
		// 已经记进 res.Steps 的失败会由调用方汇总；这里是流程性失败
		// （例如连不上代理），补一条记录再返回，避免只剩一句没有上下文的错误。
		res.Steps = append(res.Steps, Step{Name: "执行链路", Detail: err.Error()})
		return res, nil
	}
	return res, nil
}

// runner 持有一次验收的运行时状态。
type runner struct {
	opts Options
	res  *Result

	// session 是 MCP 会话，握手成功后才有。
	session *mcp.ClientSession
	// deviceID 是本机设备 ID，来自 /api/status。
	deviceID string
	// project 是云端为临时项目分配的 ID，清理时按它删除登记。
	project string
	// wrote 表示确实写入了临时 bundle，清理时才需要删。
	wrote bool
	// projectsBackup 是本机项目登记的原始内容，清理时原样写回。
	projectsBackup []byte
}

// run 是主流程，返回 error 表示无法继续。
func (r *runner) run(ctx context.Context) error {
	if err := r.checkAdmin(ctx); err != nil {
		return err
	}
	if err := r.checkStatus(ctx); err != nil {
		return err
	}
	if err := r.checkHosts(ctx); err != nil {
		return err
	}
	if err := r.connect(ctx); err != nil {
		return err
	}
	if err := r.checkTools(ctx); err != nil {
		return err
	}
	if err := r.checkStatusTool(ctx); err != nil {
		return err
	}
	if err := r.checkListAssets(ctx); err != nil {
		return err
	}
	if err := r.checkReadRelkit(ctx); err != nil {
		return err
	}
	if err := r.checkWriteAndLand(ctx); err != nil {
		return err
	}
	if err := r.checkDeleteAndClean(ctx); err != nil {
		return err
	}
	return nil
}

// step 记一项检查结果并打印。
func (r *runner) step(name string, passed bool, detail string) {
	r.res.Steps = append(r.res.Steps, Step{Name: name, Passed: passed, Detail: detail})
	mark := "PASS"
	if !passed {
		mark = "FAIL"
	}
	fmt.Fprintf(r.opts.Log, "  [%s] %s", mark, name)
	if detail != "" {
		fmt.Fprintf(r.opts.Log, "：%s", detail)
	}
	fmt.Fprintln(r.opts.Log)
}

// section 打印一个阶段标题。
func (r *runner) section(title string) {
	fmt.Fprintf(r.opts.Log, "\n%s\n", title)
}

// checkAdmin 确认管理面可访问（需求 4.3）。
func (r *runner) checkAdmin(ctx context.Context) error {
	r.section("一、本机代理")
	code, body, err := r.get(ctx, "/admin")
	if err != nil {
		return fmt.Errorf("访问 /admin 失败: %w", err)
	}
	if code != http.StatusOK {
		return fmt.Errorf("/admin 返回 %d", code)
	}
	// 页面能打开不等于内容对：代理在未配置时也会返回一个引导页。
	// 要求页面里出现 MCP 入口提示，说明它确实是新代理而不是别的服务。
	if !strings.Contains(body, "Dec") {
		return errors.New("/admin 返回的内容不像代理页面")
	}
	r.step("/admin 可访问", true, "200")
	return nil
}

// checkStatus 确认 /api/status 显示 aligned（需求 4.3）。
func (r *runner) checkStatus(ctx context.Context) error {
	code, body, err := r.get(ctx, "/api/status")
	if err != nil {
		return fmt.Errorf("访问 /api/status 失败: %w", err)
	}
	if code != http.StatusOK {
		return fmt.Errorf("/api/status 返回 %d", code)
	}
	var st struct {
		Aligned    bool   `json:"aligned"`
		DeviceID   string `json:"device_id"`
		Version    string `json:"version"`
		LastError  string `json:"last_error"`
		ServerVer  string `json:"server_version"`
		RequiredV  string `json:"required_version"`
		Maintaince bool   `json:"maintenance"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		return fmt.Errorf("/api/status 不是合法 JSON: %w", err)
	}
	if st.DeviceID == "" {
		return errors.New("本机还没登记设备，无法验收")
	}
	if !st.Aligned {
		return fmt.Errorf("代理与服务端版本未对齐：本机 %s，服务端要求 %s", st.Version, st.RequiredV)
	}
	if st.LastError != "" {
		return fmt.Errorf("代理上次同步出错：%s", st.LastError)
	}
	if st.Maintaince {
		return errors.New("服务端处于维护中，此刻无法验收")
	}
	r.deviceID = st.DeviceID
	r.step("代理已登记且 aligned", true,
		fmt.Sprintf("device=%s version=%s server=%s", st.DeviceID, st.Version, st.ServerVer))
	return nil
}

// checkHosts 确认各宿主的 dec 条目存在（需求 4.3）。
//
// 只在宿主确实装了的时候要求有条目：没装 Cursor 的机器上不可能有 ~/.cursor，
// 那不是缺陷。判定口径与 dec-agent mcp-config 一致。
func (r *runner) checkHosts(ctx context.Context) error {
	results := agent.InspectHosts()
	if len(results) == 0 {
		r.step("各宿主 dec 条目", false, "没有识别到任何宿主")
		return errors.New("没有识别到任何宿主")
	}
	var written, skipped, failed []string
	for _, res := range results {
		switch res.Status {
		case agent.HostWritten:
			// 条目必须指向本机 MCP 入口，指向别处等于没接上。
			if !strings.Contains(res.Entry, "/mcp") {
				failed = append(failed, fmt.Sprintf("%s 的条目不含 /mcp: %s", res.Name, res.Entry))
				continue
			}
			written = append(written, res.Name)
		case agent.HostSkipped:
			skipped = append(skipped, fmt.Sprintf("%s(%s)", res.Name, res.Reason))
		default:
			failed = append(failed, fmt.Sprintf("%s: %s", res.Name, res.Reason))
		}
	}
	if len(failed) > 0 {
		r.step("各宿主 dec 条目", false, strings.Join(failed, "; "))
		return errors.New("有宿主的条目写入失败：" + strings.Join(failed, "; "))
	}
	if len(written) == 0 {
		r.step("各宿主 dec 条目", false, "没有任何宿主写入成功")
		return errors.New("没有任何宿主写入 dec 条目")
	}
	detail := "已写入 " + strings.Join(written, ",")
	if len(skipped) > 0 {
		detail += "；已跳过 " + strings.Join(skipped, ",")
	}
	r.step("各宿主 dec 条目", true, detail)
	return nil
}

// connect 建立 MCP 连接（需求 4.1 的握手）。
func (r *runner) connect(ctx context.Context) error {
	r.section("二、MCP 协议")
	endpoint := strings.TrimSuffix(r.opts.BaseURL, "/") + "/mcp"
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "dec-acceptance",
		Version: "1.0.0",
	}, nil)
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, err := client.Connect(dialCtx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		r.step("MCP 握手", false, err.Error())
		return fmt.Errorf("连接 %s 失败: %w", endpoint, err)
	}
	r.session = session

	init := session.InitializeResult()
	if init == nil {
		r.step("MCP 握手", false, "没有拿到 initialize 结果")
		return errors.New("没有拿到 initialize 结果")
	}
	name := ""
	if init.ServerInfo != nil {
		name = init.ServerInfo.Name
	}
	if name != "dec" {
		r.step("MCP 握手", false, "serverInfo 名称应为 dec，实际 "+name)
		return errors.New("serverInfo 名称不是 dec")
	}
	r.step("MCP 握手", true, fmt.Sprintf("serverInfo=%s protocol=%s", name, init.ProtocolVersion))
	return nil
}

// checkTools 检查 tools/list（需求 4.1）。
func (r *runner) checkTools(ctx context.Context) error {
	res, err := r.session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("tools/list 失败: %w", err)
	}
	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}

	want := []string{
		"dec_status",
		"dec_list_assets", "dec_read_asset", "dec_write_asset",
		"dec_delete_asset_item", "dec_delete_bundle",
		"dec_list_secrets", "dec_put_secret", "dec_delete_secret",
		"dec_history",
		"dec_list_projects", "dec_put_project", "dec_delete_project",
		"dec_list_subscriptions", "dec_set_subscriptions",
	}
	var missing []string
	for _, name := range want {
		tool, ok := got[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if strings.TrimSpace(tool.Description) == "" {
			missing = append(missing, name+"(无描述)")
		}
		if tool.InputSchema == nil {
			missing = append(missing, name+"(无输入 schema)")
		}
	}
	if len(missing) > 0 {
		r.step("tools/list 含全部预期工具", false, "缺少或不合格："+strings.Join(missing, ","))
		return errors.New("工具清单不完整")
	}
	r.step("tools/list 含全部预期工具", true, fmt.Sprintf("%d 个", len(want)))

	// 被禁工具必须不可见（需求 1.5）。
	banned := []string{
		"dec_read_secret", "dec_devices", "dec_revoke_device",
		"dec_rollback", "dec_rotate_root_key", "dec_set_device_kind",
	}
	var visible []string
	for _, name := range banned {
		if _, ok := got[name]; ok {
			visible = append(visible, name)
		}
	}
	if len(visible) > 0 {
		r.step("被禁工具不可见", false, "仍然可见："+strings.Join(visible, ","))
		return errors.New("被禁工具仍然可见")
	}
	r.step("被禁工具不可见", true, "读明文/设备管理/回滚/轮换均不在清单里")
	return nil
}

// callTool 调用一个工具，返回解析后的 JSON；工具报错时返回原始文本错误。
func (r *runner) callTool(ctx context.Context, name string, args map[string]any) (map[string]any, string) {
	res, err := r.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, "调用失败：" + err.Error()
	}
	text := contentText(res)
	if res.IsError {
		return nil, text
	}
	if res.StructuredContent != nil {
		if m, ok := res.StructuredContent.(map[string]any); ok {
			return m, ""
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, "返回内容不是 JSON：" + text
	}
	return out, ""
}

// checkStatusTool 通过 dec_status 确认云端可达（需求 4.1 的状态查询）。
func (r *runner) checkStatusTool(ctx context.Context) error {
	out, errMsg := r.callTool(ctx, "dec_status", map[string]any{})
	if errMsg != "" {
		r.step("dec_status", false, errMsg)
		return errors.New("dec_status 失败：" + errMsg)
	}
	// hello 返回的是对外状态：只要拿到 required_version 就说明云端应答了。
	required, _ := out["required_version"].(string)
	server, _ := out["server_version"].(string)
	if server == "" && required == "" {
		r.step("dec_status", false, "没有返回服务端版本")
		return errors.New("dec_status 没有返回服务端版本")
	}
	aligned, _ := out["aligned"].(bool)
	if !aligned {
		r.step("dec_status", false, "代理与服务端未对齐")
		return errors.New("dec_status 报告未对齐")
	}
	r.step("dec_status", true, fmt.Sprintf("server=%s required=%s", server, required))
	return nil
}

// checkListAssets 确认能列出资产（需求 4.1）。
func (r *runner) checkListAssets(ctx context.Context) error {
	out, errMsg := r.callTool(ctx, "dec_list_assets", map[string]any{})
	if errMsg != "" {
		r.step("dec_list_assets", false, errMsg)
		return errors.New("dec_list_assets 失败：" + errMsg)
	}
	bundles, _ := out["bundles"].([]any)
	if len(bundles) == 0 {
		r.step("dec_list_assets", false, "没有任何 bundle")
		return errors.New("资产列表为空")
	}
	var names []string
	for _, b := range bundles {
		if m, ok := b.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	r.step("dec_list_assets", true, fmt.Sprintf("%d 个 bundle：%s", len(names), strings.Join(names, ",")))
	return nil
}

// checkReadRelkit 读取 relkit（需求 4.1 明确要求读这个 bundle）。
func (r *runner) checkReadRelkit(ctx context.Context) error {
	out, errMsg := r.callTool(ctx, "dec_read_asset", map[string]any{"bundle": "relkit"})
	if errMsg != "" {
		r.step("读取 relkit", false, errMsg)
		return errors.New("读取 relkit 失败：" + errMsg)
	}
	files, _ := out["files"].([]any)
	if len(files) == 0 {
		r.step("读取 relkit", false, "没有读到任何文件")
		return errors.New("relkit 内容为空")
	}
	// 正文里不能出现明文密钥：占位符应当保持原样（需求 1.5）。
	for _, f := range files {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := m["content"].(string); ok && strings.Contains(c, "PRIVATE KEY-----") {
			r.step("读取 relkit", false, "返回内容里出现了疑似私钥明文")
			return errors.New("relkit 内容里出现私钥明文")
		}
	}
	r.step("读取 relkit", true, fmt.Sprintf("%d 个文件", len(files)))
	return nil
}

// checkWriteAndLand 写入临时 bundle 并确认它落到本机（需求 4.1）。
func (r *runner) checkWriteAndLand(ctx context.Context) error {
	r.section("三、写入与落地")

	// 先登记一个临时项目，让条目落在项目平面而不是用户的 IDE 目录里。
	// 这样验收不会改动用户真实的 skill/rule 文件。
	if err := r.registerTempProject(ctx); err != nil {
		return err
	}

	marker := fmt.Sprintf("dec acceptance %s", time.Now().UTC().Format(time.RFC3339Nano))
	manifest := fmt.Sprintf(`name: %s
items:
  - kind: rule
    path: acceptance.md
    name: acceptance
    planes: [project]
`, r.opts.Bundle)
	args := map[string]any{
		"bundle":  r.opts.Bundle,
		"manifest": manifest,
		"items": []any{
			map[string]any{"path": "acceptance.md", "content": marker + "\n"},
		},
		"message": "验收：写入临时 bundle " + r.opts.Bundle,
	}
	if _, errMsg := r.callTool(ctx, "dec_write_asset", args); errMsg != "" {
		r.step("写入临时 bundle", false, errMsg)
		return errors.New("写入临时 bundle 失败：" + errMsg)
	}
	r.wrote = true
	r.step("写入临时 bundle", true, r.opts.Bundle)

	// 确认落地：等服务端把内容推到本机，读回文件比对 marker。
	landed, err := r.waitForFile(ctx, marker)
	if err != nil {
		r.step("临时 bundle 落地本机", false, err.Error())
		return err
	}
	r.step("临时 bundle 落地本机", true, landed)
	return nil
}

// checkDeleteAndClean 删除临时 bundle 并确认落点被清理（需求 4.1、4.2）。
func (r *runner) checkDeleteAndClean(ctx context.Context) error {
	r.section("四、清理")
	if _, errMsg := r.callTool(ctx, "dec_delete_bundle", map[string]any{
		"bundle":  r.opts.Bundle,
		"message": "验收：删除临时 bundle " + r.opts.Bundle,
	}); errMsg != "" {
		r.step("删除临时 bundle", false, errMsg)
		return errors.New("删除临时 bundle 失败：" + errMsg)
	}
	r.wrote = false
	r.step("删除临时 bundle", true, r.opts.Bundle)

	// 确认列表里不再有它：只有目录被真正删掉才会消失。
	if err := r.waitBundleGone(ctx); err != nil {
		r.step("临时 bundle 已从列表消失", false, err.Error())
		return err
	}
	r.step("临时 bundle 已从列表消失", true, "")

	// 确认落点被清理：订阅减少后云端会下发删除指令。
	if err := r.waitFileGone(ctx); err != nil {
		r.step("落点已清理", false, err.Error())
		return err
	}
	r.step("落点已清理", true, "")
	return nil
}

// registerTempProject 登记临时项目。
//
// 项目必须先在本机登记，云端才知道这个项目在这台机器上的根路径；
// 否则项目平面的落点无法展开，条目根本落不下来（代理会报 not allowed）。
//
// ID 由云端生成后回传，本机再照抄：项目 ID 的服务端规则是
// 「设备 + 路径」的哈希，如果这里自己算一遍，规则一改两边就对不上，
// 而症状是「项目登记了但文件不落地」，极难排查。
func (r *runner) registerTempProject(ctx context.Context) error {
	created, errMsg := r.callTool(ctx, "dec_put_project", map[string]any{
		"device_id": r.deviceID,
		"name":      "dec-acceptance",
		"path":      r.opts.ProjectPath,
		"bundles":   []any{r.opts.Bundle},
	})
	if errMsg != "" {
		return errors.New("云端登记项目失败：" + errMsg)
	}
	id, _ := created["id"].(string)
	if id == "" {
		return errors.New("云端没有返回项目 ID")
	}
	r.project = id

	projects, err := agent.LoadProjects(r.stateDir())
	if err != nil {
		return fmt.Errorf("读取本机项目登记失败: %w", err)
	}
	// 备份原始文件，清理时原样写回，连格式都不变。
	if raw, err := os.ReadFile(filepath.Join(r.stateDir(), "projects.json")); err == nil {
		r.projectsBackup = raw
	}
	if err := os.MkdirAll(r.opts.ProjectPath, 0o755); err != nil {
		return fmt.Errorf("创建临时项目目录失败: %w", err)
	}

	keep := make([]agent.Project, 0, len(projects)+1)
	for _, p := range projects {
		if p.ID != id {
			keep = append(keep, p)
		}
	}
	keep = append(keep, agent.Project{ID: id, Path: r.opts.ProjectPath})
	if err := agent.SaveProjects(r.stateDir(), keep); err != nil {
		return fmt.Errorf("登记本机临时项目失败: %w", err)
	}
	// 通过回环接口再写一次，让运行中的代理立即加载新登记（它会重算解析器）。
	body, _ := json.Marshal(keep)
	if code, _, err := r.post(ctx, "/api/projects", string(body)); err != nil || code != http.StatusOK {
		return fmt.Errorf("告知运行中的代理项目登记失败: code=%d err=%v", code, err)
	}
	return nil
}

// cleanup 尽最大努力清掉本次验收产生的一切（需求 4.2）。
//
// 顺序是「云端 → 本机代理 → 磁盘」：先让云端不再期望这些内容，
// 代理才会收到删除指令；反过来先删磁盘会让云端记录与实际不一致。
//
// 只回收自己造出来的东西。设备订阅不在其中——清单里写了什么就恢复成什么，
// 绝不能用「清空」当默认值：那会把用户真实的订阅一起抹掉。
func (r *runner) cleanup(ctx context.Context) {
	if r.session == nil {
		// 握手都没成功，什么也没写进去。
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()

	// 1. 删掉临时 bundle（即使主流程已经删过，这里再删一次也应当成功）。
	if _, errMsg := r.callTool(ctx, "dec_delete_bundle", map[string]any{
		"bundle":  r.opts.Bundle,
		"message": "验收清理",
	}); errMsg != "" {
		r.res.Residue = append(r.res.Residue, "临时 bundle "+r.opts.Bundle+"：删除失败 "+errMsg)
	}

	// 2. 删掉云端临时项目登记。它会带走这个项目的订阅关系，
	// 设备自身的订阅列表没有被改过，不需要恢复。
	if r.project != "" {
		if _, errMsg := r.callTool(ctx, "dec_delete_project", map[string]any{"id": r.project}); errMsg != "" {
			r.res.Residue = append(r.res.Residue, "云端项目登记 "+r.project+" 删除失败："+errMsg)
		}
	}

	// 3. 恢复本机项目登记。
	if err := r.restoreLocalProjects(ctx); err != nil {
		r.res.Residue = append(r.res.Residue, "本机项目登记恢复失败："+err.Error())
	}

	// 4. 检查残留：临时目录是否还在。
	if _, err := os.Stat(r.opts.ProjectPath); err == nil && !r.opts.Keep {
		if rmErr := os.RemoveAll(r.opts.ProjectPath); rmErr != nil {
			r.res.Residue = append(r.res.Residue, "临时项目目录删除失败："+rmErr.Error())
		}
	}
}

// restoreLocalProjects 把本机项目登记恢复成验收前的样子。
func (r *runner) restoreLocalProjects(ctx context.Context) error {
	path := filepath.Join(r.stateDir(), "projects.json")
	if r.projectsBackup == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := os.WriteFile(path, r.projectsBackup, 0o600); err != nil {
		return err
	}
	// 让运行中的代理也回到原状，否则它会带着临时项目继续跑。
	var ps []agent.Project
	if r.projectsBackup != nil {
		if err := json.Unmarshal(r.projectsBackup, &ps); err != nil {
			return err
		}
	}
	body, _ := json.Marshal(ps)
	if code, _, err := r.post(ctx, "/api/projects", string(body)); err != nil || code != http.StatusOK {
		return fmt.Errorf("code=%d err=%v", code, err)
	}
	return nil
}

// waitForFile 等文件出现并含指定内容。
func (r *runner) waitForFile(ctx context.Context, marker string) (string, error) {
	deadline := time.Now().Add(r.opts.Timeout)
	path := r.landingPath()
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), marker) {
			return path, nil
		}
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("%s 超时未出现（或内容不含标记），等待 %s", path, r.opts.Timeout)
}

// waitFileGone 等落点被删除。
func (r *runner) waitFileGone(ctx context.Context) error {
	deadline := time.Now().Add(r.opts.Timeout)
	path := r.landingPath()
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("%s 超时未被清理，等待 %s", path, r.opts.Timeout)
}

// waitBundleGone 等临时 bundle 从资产列表消失。
func (r *runner) waitBundleGone(ctx context.Context) error {
	deadline := time.Now().Add(r.opts.Timeout)
	for time.Now().Before(deadline) {
		out, errMsg := r.callTool(ctx, "dec_list_assets", map[string]any{})
		if errMsg == "" {
			bundles, _ := out["bundles"].([]any)
			found := false
			for _, b := range bundles {
				if m, ok := b.(map[string]any); ok && m["name"] == r.opts.Bundle {
					found = true
				}
			}
			if !found {
				return nil
			}
		}
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("%s 仍在资产列表里，等待 %s", r.opts.Bundle, r.opts.Timeout)
}

// landingPath 返回临时 bundle 在项目平面的落点。
func (r *runner) landingPath() string {
	// rule 条目落在项目根的 .cursor/rules/ 下；manifest 里没指定 host，
	// 因此全部宿主都会收到，这里比对其中一个即可。
	return filepath.Join(r.opts.ProjectPath, ".cursor", "rules", "acceptance.md")
}

// projectID 已移除：项目 ID 由云端生成后回传（见 registerTempProject）。
// 客户端复刻一遍云端规则看似省一次往返，但规则一改两边就静默错位，
// 而症状是「项目登记成功但文件不落地」，排查成本远高于这次往返。

// stateDir 返回代理状态目录。
func (r *runner) stateDir() string {
	if dir := os.Getenv("DEC_AGENT_STATE"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".dec-agent"
	}
	return filepath.Join(home, ".dec-agent")
}

// get 发一个 GET 请求。
func (r *runner) get(ctx context.Context, path string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(r.opts.BaseURL, "/")+path, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}

// post 发一个 JSON POST 请求。
func (r *runner) post(ctx context.Context, path, body string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(r.opts.BaseURL, "/")+path, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}

// sleepCtx 是可被取消的等待。
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// contentText 取结果里的文本内容。
func contentText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// SortedNames 返回排序后的名字列表，供调用方稳定输出。
func SortedNames(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
