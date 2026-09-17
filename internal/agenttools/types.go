// Package agenttools 是 Agent MCP 工具清单与调用编排的唯一真相源（ADR 0030）。
//
// dec-mcp 只读清单并转发；Console 执行步骤；schema / Plan 都在本包生成，
// 不再编译进 stdio 壳。
package agenttools

import "encoding/json"

// Protocol 是清单协议版本。壳只校验这个数字；与 Console SemVer 无关。
const Protocol = 1

// BootstrapTool 是清单缺失时壳内置的唯一工具，调它会拉起 Console 并触发 dump。
const BootstrapTool = "dec_console_status"

// Owner 区分谁处理 tools/call。
const (
	OwnerConsole = "console" // 未连接也可：hello / connections / connect
	OwnerServer  = "server"  // 经 plan_agent_tool → 逐步 Invoke/Run
)

// Shape 描述 Console 如何把步骤结果组装成工具返回值。
const (
	ShapeSingle = "single" // 单步结果即 data
	ShapePlanes = "planes" // {"planes":[{plane,ok,result,error},...]}
	ShapeKeyed  = "keyed"  // 按 step.key 组装，再合并 Envelope
)

// StepKind 是计划步骤的执行方式。
const (
	StepInvoke                 = "invoke"
	StepRun                    = "run"
	StepConsoleActiveOperation = "console_active_operation"
	// StepError 是 Plan 预检失败的占位步（both 时某一平面缺 project_root）。
	// Method 字段承载错误文案；Console 不得对其发 RPC。
	StepError = "error"
)

// Manifest 是写入 ~/.dec/run/agent-tools.json 的清单。
type Manifest struct {
	Protocol int           `json:"protocol"`
	Version  string        `json:"version"`
	Tools    []ToolListing `json:"tools"`
}

// ToolListing 是清单里的一条工具（壳动态 AddTool 用）。
type ToolListing struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Owner       string          `json:"owner"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Step 是 plan_agent_tool 返回的一步。
type Step struct {
	Kind        string          `json:"kind"`
	Method      string          `json:"method,omitempty"`
	ProjectRoot string          `json:"project_root,omitempty"`
	Plane       string          `json:"plane,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Key         string          `json:"key,omitempty"`
	Optional    bool            `json:"optional,omitempty"`
}

// PlanResult 是 plan_agent_tool 的返回值。
type PlanResult struct {
	Name      string         `json:"name"`
	Owner     string         `json:"owner"`
	Shape     string         `json:"shape"`
	Steps     []Step         `json:"steps"`
	Envelope  map[string]any `json:"envelope,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// PlanRequest 是 plan_agent_tool 的 payload。
type PlanRequest struct {
	Name      string          `json:"Name"`
	Arguments json.RawMessage `json:"Arguments"`
}
