package agenttools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// BuildManifest 生成可落盘的 Agent 工具清单。
// Version 形如 "v1.13.73#a1b2c3d4"：SemVer 便于人读，短摘要保证同版本改 schema 也会触发壳重载。
func BuildManifest(version string) (*Manifest, error) {
	tools := catalog()
	listings := make([]ToolListing, 0, len(tools))
	for _, t := range tools {
		schema, err := schemaFor(t.Sample)
		if err != nil {
			return nil, fmt.Errorf("工具 %s: %w", t.Name, err)
		}
		listings = append(listings, ToolListing{
			Name:        t.Name,
			Description: t.Description,
			Owner:       t.Owner,
			InputSchema: schema,
		})
	}
	body, err := json.Marshal(listings)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return &Manifest{
		Protocol: Protocol,
		Version:  fmt.Sprintf("%s#%s", version, hex.EncodeToString(sum[:8])),
		Tools:    listings,
	}, nil
}

// DumpJSON 序列化清单为 JSON（stdout / 文件用）。
func DumpJSON(version string) ([]byte, error) {
	m, err := BuildManifest(version)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, "", "  ")
}

// ParseManifest 校验并解析清单。
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("解析 agent-tools.json 失败: %w", err)
	}
	if m.Protocol != Protocol {
		return nil, fmt.Errorf("agent-tools protocol %d 不受支持（壳支持 %d）", m.Protocol, Protocol)
	}
	if len(m.Tools) == 0 {
		return nil, fmt.Errorf("agent-tools.json 没有工具")
	}
	return &m, nil
}

// BootstrapManifest 清单缺失时壳内置的最小清单（仅 console_status）。
func BootstrapManifest(version string) *Manifest {
	schema, _ := schemaFor(emptyParams{})
	return &Manifest{
		Protocol: Protocol,
		Version:  version,
		Tools: []ToolListing{{
			Name:        BootstrapTool,
			Description: "查看 Console 网关与当前连接。清单尚未生成时这是唯一可用工具；调用后 Console 会写出完整工具表。",
			Owner:       OwnerConsole,
			InputSchema: schema,
		}},
	}
}
