package secrets

import (
	"fmt"
	"path"
	"strings"
)

// 点类型目录：统一识别与 BW 侧规范名前缀。
const (
	TypeDirGCM    = ".gcm"
	TypeDirEnv    = ".env"
	TypeDirSSHKey = ".sshkey"
)

// SecretTypeID 是点类型目录对应的稳定 id（去点）。
type SecretTypeID string

const (
	SecretTypeGCM    SecretTypeID = "gcm"
	SecretTypeEnv    SecretTypeID = "env"
	SecretTypeSSHKey SecretTypeID = "sshkey"
	SecretTypePlain  SecretTypeID = "note" // 非点目录普通 Note
)

// SecretType 描述一种点类型目录的识别契约（由 Processor 派生）。
type SecretType struct {
	ID       SecretTypeID
	Dir      string // 含点前缀，如 .gcm；普通 note 为空
	Source   string // note | ssh_item | note_env
	Template string // Remote 登记预填正文；空表示无预填
}

const defaultGCMTemplate = `host: example.com
username: user
password: "token"
# protocol: https
# provider: generic
# path: owner/repository  # project 可省略并从 origin 推导
`

// RegisteredSecretTypes 返回点类型表（不含普通 note；识别/迁移用）。
func RegisteredSecretTypes() []SecretType {
	out := make([]SecretType, 0, len(registeredProcessors))
	for _, p := range registeredProcessors {
		if p.Dir == "" {
			continue
		}
		out = append(out, p.AsSecretType())
	}
	return out
}

// LookupSecretTypeByDir 按点目录名查找（如 ".gcm"）。
func LookupSecretTypeByDir(dir string) (SecretType, bool) {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return SecretType{}, false
	}
	p, ok := LookupProcessorByDir(dir)
	if !ok || p.Dir == "" {
		return SecretType{}, false
	}
	return p.AsSecretType(), true
}

// LookupSecretTypeByID 按类型 id 查找（如 "gcm"）；不含普通 note。
func LookupSecretTypeByID(id string) (SecretType, bool) {
	p, ok := LookupProcessor(id)
	if !ok || p.Dir == "" {
		return SecretType{}, false
	}
	return p.AsSecretType(), true
}

// TypePath 是解析后的点类型路径。
type TypePath struct {
	Type     SecretType
	Dir      string // .gcm
	Rest     string // cnb.yaml 或 deploy
	Full     string // .gcm/cnb.yaml
	Instance string // 对 gcm：文件 stem；对 sshkey：实例名
}

// ParseTypePath 解析相对同步根（或 SSH Item 名）的点类型路径。
// 首段以 "." 开头但未注册 → 返回 error（未知点目录硬失败）。
// 非点目录路径 ok=false（普通 Note），err=nil。
func ParseTypePath(raw string) (tp TypePath, ok bool, err error) {
	rel := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return TypePath{}, false, nil
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return TypePath{}, false, fmt.Errorf("非法类型路径: %q", raw)
	}
	parts := strings.Split(clean, "/")
	first := parts[0]
	if !strings.HasPrefix(first, ".") {
		return TypePath{}, false, nil
	}
	st, found := LookupSecretTypeByDir(first)
	if !found {
		return TypePath{}, false, fmt.Errorf("未知点类型目录 %q（路径 %q）", first, clean)
	}
	rest := strings.Join(parts[1:], "/")
	if rest == "" || rest == "." {
		return TypePath{}, false, fmt.Errorf("%s 下缺少实例名: %q", first, clean)
	}
	instance := rest
	if st.ID == SecretTypeGCM {
		base := path.Base(rest)
		instance = strings.TrimSuffix(base, path.Ext(base))
		if instance == "" {
			return TypePath{}, false, fmt.Errorf("gcm 实例名为空: %q", clean)
		}
	}
	if st.ID == SecretTypeSSHKey {
		if strings.Contains(rest, "/") {
			return TypePath{}, false, fmt.Errorf("sshkey 名不能含多级路径: %q", clean)
		}
		instance = rest
	}
	return TypePath{
		Type:     st,
		Dir:      first,
		Rest:     rest,
		Full:     clean,
		Instance: instance,
	}, true, nil
}

// SuggestNotePath 给出 Remote 登记时的建议相对路径。
func SuggestNotePath(typeID SecretTypeID, instance string) string {
	instance = strings.TrimSpace(instance)
	switch typeID {
	case SecretTypeGCM:
		if instance == "" {
			instance = "host"
		}
		return path.Join(TypeDirGCM, instance+".yaml")
	case SecretTypeEnv:
		if instance == "" {
			instance = "app"
		}
		if !strings.HasSuffix(strings.ToLower(instance), ".env") {
			instance += ".env"
		}
		return path.Join(TypeDirEnv, instance)
	case SecretTypeSSHKey:
		if instance == "" {
			instance = "deploy"
		}
		return CanonicalSSHKeyName(instance)
	default:
		return instance
	}
}

// CanonicalSSHKeyName 把实例名规范为 `.sshkey/<实例>`。
func CanonicalSSHKeyName(instance string) string {
	instance = strings.TrimSpace(instance)
	return path.Join(TypeDirSSHKey, instance)
}

// SSHKeyInstance 从 BW SSH Item 名取出落地用实例名。
// 必须是 `.sshkey/<实例>`；裸名或其它形态返回 error。
func SSHKeyInstance(fullName string) (string, error) {
	tp, ok, err := ParseTypePath(fullName)
	if err != nil {
		return "", err
	}
	if !ok || tp.Type.ID != SecretTypeSSHKey {
		return "", fmt.Errorf("SSH Key 名必须为 %s/<实例>，收到 %q", TypeDirSSHKey, fullName)
	}
	if _, err := validateSSHSafeName("SSH Key 实例", tp.Instance); err != nil {
		return "", err
	}
	return tp.Instance, nil
}

// MigrateLegacyGitGCMPath 把旧 `*_gitgcm.yaml` 映射到 `.gcm/<实例>.yaml`。
// 非旧约定返回 ok=false。
func MigrateLegacyGitGCMPath(noteRel string) (newPath string, ok bool) {
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(noteRel), "\\", "/"))
	lower := strings.ToLower(base)
	var stem string
	switch {
	case strings.HasSuffix(lower, ".yaml"):
		stem = base[:len(base)-len(".yaml")]
	case strings.HasSuffix(lower, ".yml"):
		stem = base[:len(base)-len(".yml")]
	default:
		return "", false
	}
	const suffix = "_gitgcm"
	if !strings.HasSuffix(strings.ToLower(stem), suffix) {
		return "", false
	}
	inst := stem[:len(stem)-len(suffix)]
	if inst == "" || strings.ContainsAny(inst, `/\`) {
		return "", false
	}
	// 保留原扩展名风格：统一用 .yaml
	return path.Join(TypeDirGCM, inst+".yaml"), true
}

// MigrateLegacyEnvPath 把旧 `env/<name>.env` 映射到 `.env/<name>.env`。
func MigrateLegacyEnvPath(noteRel string) (newPath string, ok bool) {
	rel := strings.ReplaceAll(strings.TrimSpace(noteRel), "\\", "/")
	rel, err := normalizeSyncRelPath(rel)
	if err != nil {
		return "", false
	}
	dir, base := path.Split(rel)
	dir = strings.Trim(dir, "/")
	if dir != "env" || !strings.HasSuffix(strings.ToLower(base), ".env") {
		return "", false
	}
	return path.Join(TypeDirEnv, base), true
}

// NeedsLegacySSHKeyMigrate 判断 SSH Item 名是否仍是裸名（需迁到 .sshkey/）。
func NeedsLegacySSHKeyMigrate(fullName string) bool {
	name := strings.TrimSpace(fullName)
	if name == "" {
		return false
	}
	if _, err := SSHKeyInstance(name); err == nil {
		return false
	}
	// 已是未知点目录等 → 不在此当「裸名迁移」
	if strings.HasPrefix(name, ".") {
		return false
	}
	_, err := validateSSHSafeName("SSH Key", name)
	return err == nil
}
