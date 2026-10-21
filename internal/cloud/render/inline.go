package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// 变量替换与密钥内联（需求 4.1）。
//
// 两种占位符：
//
//	{{secret:名称}}      密钥明文，渲染时解密
//	{{device.id}} 等     设备上下文变量，渲染时取值，不涉及密钥
//
// 渲染是“全有或全无”的：任何一个密钥取不到，整条条目都不渲染，
// 由调用方保留上一次成功的结果（需求 4.5）。绝不用空值兜底。

// ErrRender 表示渲染失败。调用方据此保留上次结果并告警。
var ErrRender = errors.New("render failed")

// secretRefRe 与 assets 包里的语法保持一致：名称只允许小写字母数字与 . _ -。
var secretRefRe = regexp.MustCompile(`\{\{\s*secret:([a-z0-9][a-z0-9._-]*)\s*\}\}`)

// deviceVarRe 匹配 {{device.xxx}} 这类设备变量。
var deviceVarRe = regexp.MustCompile(`\{\{\s*(device\.[a-z]+)\s*\}\}`)

// context 是一次渲染的上下文。
type context struct {
	deviceID   string
	deviceName string
	os         string
	arch       string
	// getSecret 由 Renderer 注入，避免本文件直接耦合 assets.Writer。
	getSecret func(name string) ([]byte, error)
	// secrets 是已经解密的值，避免同一条目多次引用同一密钥时重复解密。
	secrets map[string]string
}

// newContext 构造渲染上下文。
func newContext(dev ssot.Device, getSecret func(string) ([]byte, error)) *context {
	return &context{
		deviceID:   dev.ID,
		deviceName: dev.Name,
		os:         dev.OS,
		arch:       dev.Arch,
		getSecret:  getSecret,
		secrets:    map[string]string{},
	}
}

// wipe 丢弃缓存的密钥明文引用。
//
// Go 的字符串不可变，无法就地清零；这里能做的是尽早断开引用，
// 真正的边界是“明文只存在于这次渲染的内存里”。
func (c *context) wipe() {
	c.secrets = nil
}

// render 渲染一份原始内容，返回渲染结果与是否含密钥。
func (c *context) render(raw []byte) ([]byte, bool, error) {
	// 先处理设备变量：即使内容里没有密钥，变量也要替换。
	out := deviceVarRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := deviceVarRe.FindSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if v, ok := c.deviceVar(string(sub[1])); ok {
			return []byte(v)
		}
		return m
	})

	refs := assets.ExtractSecretRefs(out)
	if len(refs) == 0 {
		return out, false, nil
	}

	var missing []string
	for _, name := range refs {
		if _, ok := c.secrets[name]; ok {
			continue
		}
		v, err := c.lookup(name)
		if err != nil {
			missing = append(missing, fmt.Sprintf("%s(%v)", name, err))
			continue
		}
		c.secrets[name] = v
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, true, fmt.Errorf("%w: 密钥不可用: %s", ErrRender, strings.Join(missing, "; "))
	}

	out = secretRefRe.ReplaceAllFunc(out, func(m []byte) []byte {
		sub := secretRefRe.FindSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return []byte(c.secrets[string(sub[1])])
	})
	return out, true, nil
}

// lookup 取一条密钥的明文。
func (c *context) lookup(name string) (string, error) {
	if c.getSecret == nil {
		return "", errors.New("未配置密钥读取")
	}
	b, err := c.getSecret(name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// deviceVar 返回设备上下文变量的值。
func (c *context) deviceVar(name string) (string, bool) {
	switch name {
	case "device.id":
		return c.deviceID, true
	case "device.name":
		return c.deviceName, true
	case "device.os":
		return c.os, true
	case "device.arch":
		return c.arch, true
	}
	return "", false
}

// mcpserverOf 从 MCP 条目的原始内容里取出单个 server 定义，用于 json_key 下发。
//
// 支持两种书写方式，减少迁移时的模板改动：
//
//	{ "mcpServers": { "名称": {...} } }   旧格式，取 mcpServers 下的唯一项
//	{ "command": "...", "args": [...] }   直接就是一个 server 定义
func mcpserverOf(raw []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: MCP 条目内容为空", assets.ErrSchema)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil, fmt.Errorf("%w: MCP 条目不是合法 JSON: %v", assets.ErrSchema, err)
	}
	if inner, ok := probe["mcpServers"]; ok {
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(inner, &servers); err != nil {
			return nil, fmt.Errorf("%w: mcpServers 不是对象: %v", assets.ErrSchema, err)
		}
		if len(servers) != 1 {
			return nil, fmt.Errorf("%w: MCP 条目必须只定义一个 server，实际 %d 个",
				assets.ErrSchema, len(servers))
		}
		for _, v := range servers {
			return v, nil
		}
	}
	return json.RawMessage(trimmed), nil
}
