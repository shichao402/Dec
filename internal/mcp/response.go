package mcp

// logEntry 是写入 MCP tool 响应的精简日志行。
type logEntry struct {
	Level   string `json:"level"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

type toolResponse struct {
	OK    bool       `json:"ok"`
	Error string     `json:"error,omitempty"`
	Data  any        `json:"data,omitempty"`
	Logs  []logEntry `json:"logs,omitempty"`
}

func toolOK(data any, logs []logEntry) (*struct{}, toolResponse, error) {
	return nil, toolResponse{OK: true, Data: data, Logs: logs}, nil
}

func toolFail(err error, logs []logEntry) (*struct{}, toolResponse, error) {
	return nil, toolResponse{OK: false, Error: err.Error(), Logs: logs}, nil
}
