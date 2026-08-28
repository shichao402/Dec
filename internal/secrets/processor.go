package secrets

import (
	"fmt"
	"strings"
)

// SourceMode 是 Processor 声明的 Remote 登记素材来源。
// TUI 只轮转 Processor.SourceModes，不按类型硬编码分支。
type SourceMode string

const (
	SourceTemp     SourceMode = "temp"     // 外部编辑器（Secure Note）
	SourcePath     SourceMode = "path"     // 手输本地路径
	SourceGenerate SourceMode = "generate" // 本机生成（SSH Key）
	SourcePicker   SourceMode = "picker"   // 系统文件选择器 → 归一为 path
)

// SourceKindNote / SourceKindSSHItem 对齐 Bitwarden 写入器。
const (
	SourceKindNote    = "note"
	SourceKindSSHItem = "ssh_item"
)

// Processor 是同级 Secret 类型：决定名称规则、登记来源与 Bitwarden 写入种类。
// machine handler（如 GCM Apply）是可选后处理，不属于 Remote 创建链路。
type Processor struct {
	ID            SecretTypeID
	Dir           string // 点目录；普通 note 为空
	SourceKind    string // note | ssh_item
	Label         string
	Template      string // temp 预填；空表示无
	SourceModes   []SourceMode
	DefaultSource SourceMode
}

var registeredProcessors = []Processor{
	{
		ID:            SecretTypePlain,
		Dir:           "",
		SourceKind:    SourceKindNote,
		Label:         "note（任意路径）",
		SourceModes:   []SourceMode{SourceTemp, SourcePath, SourcePicker},
		DefaultSource: SourceTemp,
	},
	{
		ID:            SecretTypeGCM,
		Dir:           TypeDirGCM,
		SourceKind:    SourceKindNote,
		Label:         ".gcm（Git Credential Manager）",
		Template:      defaultGCMTemplate,
		SourceModes:   []SourceMode{SourceTemp, SourcePath, SourcePicker},
		DefaultSource: SourceTemp,
	},
	{
		ID:            SecretTypeEnv,
		Dir:           TypeDirEnv,
		SourceKind:    SourceKindNote,
		Label:         ".env（dotenv / dec-exec）",
		Template:      "# KEY=value\n",
		SourceModes:   []SourceMode{SourceTemp, SourcePath, SourcePicker},
		DefaultSource: SourceTemp,
	},
	{
		ID:            SecretTypeSSHKey,
		Dir:           TypeDirSSHKey,
		SourceKind:    SourceKindSSHItem,
		Label:         ".sshkey（BW SSH Key Item）",
		SourceModes:   []SourceMode{SourceGenerate, SourcePath, SourcePicker},
		DefaultSource: SourceGenerate,
	},
}

// RegisteredProcessors 返回同级 Processor 表（含普通 note）。
func RegisteredProcessors() []Processor {
	out := make([]Processor, len(registeredProcessors))
	copy(out, registeredProcessors)
	return out
}

// LookupProcessor 按 id 查找（如 "gcm" / "note" / "sshkey"）。
func LookupProcessor(id string) (Processor, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, p := range registeredProcessors {
		if string(p.ID) == id {
			return p, true
		}
	}
	return Processor{}, false
}

// LookupProcessorByDir 按点目录查找（如 ".gcm"）；空目录返回 note。
func LookupProcessorByDir(dir string) (Processor, bool) {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return LookupProcessor(string(SecretTypePlain))
	}
	for _, p := range registeredProcessors {
		if p.Dir == dir {
			return p, true
		}
	}
	return Processor{}, false
}

// HasSourceMode 判断该 Processor 是否声明了指定来源。
func (p Processor) HasSourceMode(mode SourceMode) bool {
	for _, m := range p.SourceModes {
		if m == mode {
			return true
		}
	}
	return false
}

// CycleSourceMode 在 Processor 声明的来源间轮转。
func (p Processor) CycleSourceMode(cur SourceMode) SourceMode {
	if len(p.SourceModes) == 0 {
		return cur
	}
	for i, m := range p.SourceModes {
		if m == cur {
			return p.SourceModes[(i+1)%len(p.SourceModes)]
		}
	}
	return p.DefaultSource
}

// SuggestName 给出 Remote 登记时的建议名。
func (p Processor) SuggestName() string {
	return SuggestNotePath(p.ID, "")
}

// NormalizeName 规范化用户输入的登记名；失败返回 error。
func (p Processor) NormalizeName(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "", fmt.Errorf("名称不能为空")
	}
	switch p.ID {
	case SecretTypePlain:
		if strings.HasPrefix(raw, ".") {
			tp, ok, err := ParseTypePath(raw)
			if err != nil {
				return "", err
			}
			if ok {
				return "", fmt.Errorf("普通 note 不要用点类型路径 %q；请改选对应类型", tp.Full)
			}
		}
		return NormalizeNoteRel(raw)
	case SecretTypeGCM, SecretTypeEnv:
		tp, ok, err := ParseTypePath(raw)
		if err != nil {
			return "", err
		}
		if !ok {
			// 允许只输实例：补全为点目录路径
			return SuggestNotePath(p.ID, raw), nil
		}
		if tp.Type.ID != p.ID {
			return "", fmt.Errorf("名称 %q 不属于类型 %s", raw, p.Dir)
		}
		return tp.Full, nil
	case SecretTypeSSHKey:
		if !strings.Contains(raw, "/") && !strings.HasPrefix(raw, ".") {
			raw = CanonicalSSHKeyName(raw)
		}
		inst, err := SSHKeyInstance(raw)
		if err != nil {
			return "", err
		}
		return CanonicalSSHKeyName(inst), nil
	default:
		return raw, nil
	}
}

// WritesSecureNote 表示该 Processor 通过 Secure Note Writer 写入 Bitwarden。
func (p Processor) WritesSecureNote() bool {
	return p.SourceKind == SourceKindNote || p.SourceKind == "note_env"
}

// WritesSSHItem 表示该 Processor 通过 SSH Key Item Writer 写入 Bitwarden。
func (p Processor) WritesSSHItem() bool {
	return p.SourceKind == SourceKindSSHItem
}

// AsSecretType 兼容旧调用点（识别层仍用 SecretType）。
func (p Processor) AsSecretType() SecretType {
	src := p.SourceKind
	if p.ID == SecretTypeEnv {
		src = "note_env"
	}
	return SecretType{
		ID:       p.ID,
		Dir:      p.Dir,
		Source:   src,
		Template: p.Template,
	}
}