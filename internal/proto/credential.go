package proto

import (
	"path"
	"regexp"
	"strings"
)

// 凭据落点（.agent/credential-landing 需求 1–3）。
//
// 代理新增两个固定落点根：ssh（用户平面，~/.ssh）与 secrets（项目平面，
// <项目>/.secrets）。这里放两侧共用的约定：路径规则、托管块标记、
// 内容规范化。渲染端与代理端必须走同一套函数，否则就会重演 jsonnorm.go
// 里记录的那种「两边哈希口径不同、每轮都判漂移」的问题。

const (
	// RootSSH 是 SSH 凭据落点根，只支持用户平面。
	RootSSH = "ssh"
	// RootSecrets 是项目 secret 文件落点根，只支持项目平面。
	RootSecrets = "secrets"

	// SSHConfigPath 是 ~/.ssh/config，只能以 managed_block 格式写入。
	SSHConfigPath = "config"
	// SSHFragmentPath 是 Dec 自己的 SSH 配置片段。
	SSHFragmentPath = "config.d/dec.conf"

	// BlockBegin 与 BlockEnd 是 managed_block 格式的托管块边界。
	//
	// 沿用旧版 config.d/dec.conf 的写法，用户看到的标记与过去一致。
	BlockBegin = "# BEGIN DEC MANAGED"
	BlockEnd   = "# END DEC MANAGED"
)

// FormatManagedBlock 表示文件里只有托管块归 Dec 所有，块外字节原样保留。
// 目前只允许用于 ssh 根的 config（需求 3.6）。
const FormatManagedBlock Format = "managed_block"

// sshKeyNameRe 限定 ~/.ssh 下 Dec 私钥与公钥的文件名。
//
// 名字直接位于 ~/.ssh 下，不允许子目录；字符集与密钥名一致。
var sshKeyNameRe = regexp.MustCompile(`^dec_[a-z0-9][a-z0-9._-]*$`)

// ValidSSHPath 报告 rel 是否是 ssh 根允许的相对路径（需求 1.2）。
func ValidSSHPath(rel string) bool {
	switch rel {
	case SSHConfigPath, SSHFragmentPath:
		return true
	}
	return sshKeyNameRe.MatchString(rel)
}

// SecretsIgnoreName 是代理在 <项目>/.secrets 下自己维护的忽略文件，不接受下发。
const SecretsIgnoreName = ".gitignore"

// ValidSecretsPath 报告 rel 是否是 secrets 根允许的相对路径（需求 2.2）。
//
// 只接受已规范化的正斜杠相对路径：不能为空、不能是绝对路径、不能上跳、
// 不能含反斜杠，也不能是代理维护的 .gitignore。
func ValidSecretsPath(rel string) bool {
	if rel == "" || rel == SecretsIgnoreName || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return false
	}
	if len(rel) >= 2 && rel[1] == ':' {
		return false
	}
	if path.Clean(rel) != rel {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// IsSSHPrivateKey 报告 ssh 根下的 rel 是否是私钥。
//
// 判定只看路径：`dec_*` 且不以 .pub 结尾。私钥必须 0600、LF 行尾，
// 并且不能加说明行（需求 1.3、1.5、4.4）。
func IsSSHPrivateKey(rel string) bool {
	return sshKeyNameRe.MatchString(rel) && !strings.HasSuffix(rel, ".pub")
}

// NormalizeSSHKey 把私钥内容统一成 LF 行尾并以换行结尾（需求 1.5）。
//
// OpenSSH 读到 CRLF 私钥时报 `error in libcrypto`，文件存在、权限正确、
// 大小也正常，极难排查。渲染端下发前与代理落盘前都调用它，
// 两侧字节因此一致，哈希也一致。
func NormalizeSSHKey(content string) string {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// NormalizeBlockBody 规范化托管块内容：LF 行尾，非空时以换行结尾，
// 并且不能含有块边界标记本身。
//
// 托管块的哈希只按块内内容算（需求 3.4），两侧都用这个函数的结果。
// 返回 false 表示内容里夹带了边界标记：写进去会让下一次解析
// 认错块的范围，必须拒绝。
func NormalizeBlockBody(content string) (string, bool) {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == BlockBegin || t == BlockEnd {
			return "", false
		}
	}
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s, true
}
