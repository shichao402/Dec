package agent

import (
	"fmt"
	"path/filepath"

	"github.com/shichao402/Dec/internal/proto"
)

// 凭据落点根（.agent/credential-landing 需求 1、2）。
//
// 与 IDE 宿主分开放：IDE 宿主按「类别目录」展开（skills/rules/env/mcp.json），
// 凭据根没有类别目录，路径规则也完全不同。混进 hosts 表会让 Detect、
// 卸载兜底扫描等遍历宿主的地方误把 ~/.ssh 当成 IDE 目录处理。
//
// 白名单仍然写死：
//
//	ssh      仅用户平面，基准 ~/.ssh，路径只能是 dec_<名字>[.pub]、config.d/dec.conf、config
//	secrets  仅项目平面，基准 <项目>/.secrets，任意深度但不得越界
const (
	// sshDir 是 SSH 根的用户级目录。
	sshDir = ".ssh"
	// secretsDir 是 secrets 根的项目级目录。
	secretsDir = ".secrets"
)

// CredentialRoots 返回代理支持的凭据根，随对账请求上报（需求 4.5）。
func CredentialRoots() []string {
	return []string{proto.RootSecrets, proto.RootSSH}
}

// isCredentialRoot 报告 root 是否是凭据根。
func isCredentialRoot(root string) bool {
	return root == proto.RootSSH || root == proto.RootSecrets
}

// resolveCredential 展开凭据根的落点。调用方已确认 root 是凭据根。
func (r *Resolver) resolveCredential(t proto.Target) (string, error) {
	if t.Path == "" {
		return "", fmt.Errorf("%w: 落点路径为空", ErrNotAllowed)
	}
	switch t.Root {
	case proto.RootSSH:
		if t.Plan != proto.PlanUser {
			return "", fmt.Errorf("%w: ssh 根只支持用户平面", ErrNotAllowed)
		}
		if !proto.ValidSSHPath(t.Path) {
			return "", fmt.Errorf("%w: ssh 根不接受路径 %q", ErrNotAllowed, t.Path)
		}
		base := filepath.Join(r.home, sshDir)
		full := filepath.Join(base, filepath.FromSlash(t.Path))
		if err := ensureWithin(base, full); err != nil {
			return "", err
		}
		return full, nil

	case proto.RootSecrets:
		if t.Plan != proto.PlanProject {
			return "", fmt.Errorf("%w: secrets 根只支持项目平面", ErrNotAllowed)
		}
		root, ok := r.projects[t.ProjectID]
		if !ok {
			return "", fmt.Errorf("%w: 项目 %q 未在本机登记", ErrNotAllowed, t.ProjectID)
		}
		if err := checkProjectRoot(r.home, root); err != nil {
			return "", err
		}
		rel, ok := cleanRel(t.Path)
		if !ok || filepath.IsAbs(t.Path) || (rel != secretsIgnoreName && !proto.ValidSecretsPath(rel)) {
			return "", fmt.Errorf("%w: secrets 根不接受路径 %q", ErrNotAllowed, t.Path)
		}
		if rel == secretsIgnoreName {
			// .secrets/.gitignore 是代理自己维护的保护文件，不能被下发覆盖。
			return "", fmt.Errorf("%w: %s 由代理维护，不接受下发", ErrNotAllowed, secretsIgnoreName)
		}
		base := filepath.Join(root, secretsDir)
		full := filepath.Join(base, filepath.FromSlash(rel))
		if err := ensureWithin(base, full); err != nil {
			return "", err
		}
		return full, nil
	}
	return "", fmt.Errorf("%w: 未知凭据根 %q", ErrNotAllowed, t.Root)
}

// credentialKind 描述一个已展开的凭据落点该怎么写。
type credentialKind int

const (
	credNone credentialKind = iota
	// credSSHPrivate 是 ~/.ssh/dec_* 私钥。
	credSSHPrivate
	// credSSHPublic 是 ~/.ssh 下其他文件（.pub、config.d/dec.conf）。
	credSSHPublic
	// credSSHConfig 是 ~/.ssh/config 托管块。
	credSSHConfig
	// credProjectSecret 是 <项目>/.secrets 下的文件。
	credProjectSecret
)

// classifyTarget 判定落点属于哪一类凭据。非凭据根返回 credNone。
func classifyTarget(t proto.Target) credentialKind {
	switch t.Root {
	case proto.RootSSH:
		switch {
		case t.Path == proto.SSHConfigPath:
			return credSSHConfig
		case proto.IsSSHPrivateKey(t.Path):
			return credSSHPrivate
		default:
			return credSSHPublic
		}
	case proto.RootSecrets:
		return credProjectSecret
	}
	return credNone
}

// classifyAbsolute 按本机绝对路径归类凭据落点。
//
// 云端的删除指令只带上次记下的绝对路径，不带 Root（见 render.Plan），
// 只看 Root 会把它们全当成普通文件：项目 secret 删完留下空目录，
// ~/.ssh/config 托管块干脆不删。所以 Root 为空时按路径落在哪里来判断。
func (r *Resolver) classifyAbsolute(full string) credentialKind {
	full = filepath.Clean(full)
	sshBase := filepath.Join(r.home, sshDir)
	if isWithin(sshBase, full) && full != filepath.Clean(sshBase) {
		rel, err := filepath.Rel(sshBase, full)
		if err != nil {
			return credNone
		}
		rel = filepath.ToSlash(rel)
		switch {
		case rel == proto.SSHConfigPath:
			return credSSHConfig
		case !proto.ValidSSHPath(rel):
			return credNone
		case proto.IsSSHPrivateKey(rel):
			return credSSHPrivate
		default:
			return credSSHPublic
		}
	}
	for _, root := range r.projects {
		base := filepath.Join(root, secretsDir)
		if isWithin(base, full) && full != filepath.Clean(base) {
			return credProjectSecret
		}
	}
	return credNone
}
