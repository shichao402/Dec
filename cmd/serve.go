package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 MCP Server 模式",
	Long: `启动 Dec 的 MCP Server 模式，供 Cursor 等 IDE 调用。

此命令通过 stdin/stdout 与 IDE 通信，使用 JSON-RPC 2.0 协议。

示例：
  dec serve`,
	RunE: runServe,
}

func init() {
	RootCmd.AddCommand(serveCmd)
}

// MCP JSON-RPC 消息类型
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

type capabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type toolsListResult struct {
	Tools []tool `json:"tools"`
}

type callToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type callToolResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func runServe(cmd *cobra.Command, args []string) error {
	scanner := bufio.NewScanner(os.Stdin)
	// 增加缓冲区大小以处理大消息
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var request jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			sendError(nil, -32700, "Parse error", err.Error())
			continue
		}

		response := handleRequest(&request)
		if response != nil {
			sendResponse(response)
		}
	}

	return scanner.Err()
}

func handleRequest(request *jsonRPCRequest) *jsonRPCResponse {
	switch request.Method {
	case "initialize":
		return handleInitialize(request)
	case "initialized":
		// 通知消息，不需要响应
		return nil
	case "tools/list":
		return handleToolsList(request)
	case "tools/call":
		return handleToolsCall(request)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &rpcError{
				Code:    -32601,
				Message: "Method not found",
				Data:    request.Method,
			},
		}
	}
}

func handleInitialize(request *jsonRPCRequest) *jsonRPCResponse {
	result := initializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: capabilities{
			Tools: &toolsCapability{
				ListChanged: false,
			},
		},
		ServerInfo: serverInfo{
			Name:    "dec",
			Version: GetVersion(),
		},
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}

func handleToolsList(request *jsonRPCRequest) *jsonRPCResponse {
	tools := []tool{
		{
			Name:        "dec_list",
			Description: "列出所有可用的 Dec 包（规则包和 MCP 工具包）",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"type": {
						Type:        "string",
						Description: "过滤包类型: rule, mcp, 或留空显示全部",
					},
				},
			},
		},
		{
			Name:        "dec_sync",
			Description: "同步项目的规则和 MCP 配置",
			InputSchema: inputSchema{
				Type: "object",
			},
		},
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  toolsListResult{Tools: tools},
	}
}

func handleToolsCall(request *jsonRPCRequest) *jsonRPCResponse {
	var params callToolParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	var result callToolResult

	switch params.Name {
	case "dec_list":
		result = executeDecList(params.Arguments)
	case "dec_sync":
		result = executeDecSync(params.Arguments)
	default:
		result = callToolResult{
			Content: []contentItem{{Type: "text", Text: fmt.Sprintf("未知的工具: %s", params.Name)}},
			IsError: true,
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}

func executeDecList(args map[string]interface{}) callToolResult {
	// TODO: 实现实际的列表逻辑
	return callToolResult{
		Content: []contentItem{{
			Type: "text",
			Text: "可用的包:\n- dec (mcp): Dec 自身的 MCP Server\n\n使用 dec sync-rules 同步配置",
		}},
	}
}

func executeDecSync(args map[string]interface{}) callToolResult {
	// TODO: 实现实际的同步逻辑
	return callToolResult{
		Content: []contentItem{{
			Type: "text",
			Text: "请在终端运行 dec sync 命令同步配置",
		}},
	}
}

func sendResponse(response *jsonRPCResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		return
	}
	fmt.Println(string(data))
}

func sendError(id interface{}, code int, message string, data interface{}) {
	response := &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	sendResponse(response)
}
