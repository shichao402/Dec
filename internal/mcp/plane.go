package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/app"
)

// planeParamDoc 解释支持 both 的工具的 plane 参数（ADR 0009 二元 scope / 平面隔离）。
// 注意：若写入 jsonschema 标签，描述不得匹配 google/jsonschema-go 的 ^[^\s]*= 前缀禁令
//（例如整段以「作用平面：project=」开头会 panic）。
const planeParamDoc = "作用平面：project（项目平面，<project>/.dec 与 <project>/.secrets，只含 scope=project 的 bundle）；" +
	"user（用户平面，~/.dec 与 ~/.dec/secrets，只含 scope=user 的 bundle，等价 dec --user）；" +
	"both（先 project 再 user 各执行一次并分别回报）。留空默认 project。"

// planeParamDocSingle 解释仅支持单平面的写操作的 plane 参数。
const planeParamDocSingle = "作用平面：project（项目平面）、user（用户平面，等价 dec --user）。" +
	"留空默认 project；此操作一次只作用一个平面，不支持 both，请显式二选一。"

// parsePlanes 解析 plane 参数为待执行平面列表；both 展开成 [project, user]。
func parsePlanes(raw string) ([]app.WorkspacePlane, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "project":
		return []app.WorkspacePlane{app.WorkspaceProject}, nil
	case "user":
		return []app.WorkspacePlane{app.WorkspaceUser}, nil
	case "both":
		return []app.WorkspacePlane{app.WorkspaceProject, app.WorkspaceUser}, nil
	default:
		return nil, fmt.Errorf("plane 非法 %q（允许 project|user|both）", raw)
	}
}

// parseSinglePlane 解析仅允许单平面的 plane 参数；both 显式报错。
func parseSinglePlane(raw string) (app.WorkspacePlane, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "project":
		return app.WorkspaceProject, nil
	case "user":
		return app.WorkspaceUser, nil
	case "both":
		return "", fmt.Errorf("该操作不支持 plane=both，请显式指定 project 或 user")
	default:
		return "", fmt.Errorf("plane 非法 %q（允许 project|user）", raw)
	}
}

// planeOutcome 是 both 分发时单个平面的结果条目。
type planeOutcome struct {
	Plane  string `json:"plane"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// dispatchPlanes 按 plane 参数执行 fn。
//
// 单平面：直接按 toolOK / toolFail 返回，与旧单平面行为一致。
// both：对 project、user 各跑一次，聚合成 {"planes":[...]}；只要有一个平面成功即 toolOK，
// 全部失败才 toolFail。各平面成败与错误在 planes[].ok / planes[].error 中如实体现，不相互连累。
func (s *Server) dispatchPlanes(
	ctx context.Context,
	raw string,
	fn func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error),
) (*mcp.CallToolResult, any, error) {
	planes, err := parsePlanes(raw)
	if err != nil {
		return toolFail(err, nil)
	}
	if len(planes) == 1 {
		reporter, logs := newCollector()
		ws := app.NewWorkspace(planes[0], s.projectRoot())
		result, runErr := fn(ctx, ws, reporter)
		if runErr != nil {
			return toolFail(runErr, logs())
		}
		return toolOK(result, logs())
	}

	outcomes := make([]planeOutcome, 0, len(planes))
	allLogs := make([]logEntry, 0)
	anyOK := false
	for _, plane := range planes {
		reporter, logs := newCollector()
		ws := app.NewWorkspace(plane, s.projectRoot())
		result, runErr := fn(ctx, ws, reporter)
		oc := planeOutcome{Plane: string(plane)}
		if runErr != nil {
			oc.Error = runErr.Error()
		} else {
			oc.OK = true
			oc.Result = result
			anyOK = true
		}
		outcomes = append(outcomes, oc)
		allLogs = append(allLogs, logs()...)
	}
	payload := map[string]any{"planes": outcomes}
	if !anyOK {
		return toolFail(fmt.Errorf("两平面均失败，详见 planes[].error"), allLogs)
	}
	return toolOK(payload, allLogs)
}
