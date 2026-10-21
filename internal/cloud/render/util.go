package render

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

// 路径与命名工具。
//
// 这里的校验都只防“写错”，不是安全边界：服务端唯一的写入者就是用户自己，
// 真正的信任边界是服务端与它的部署链路（已定决策 13）。

// normalizePath 清理项目路径。
//
// 保留绝对路径语义，去掉尾部斜杠与多余的 . / ..，
// 但不解析符号链接：那需要目标机器的文件系统，只有代理知道。
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") && !looksLikeWindowsAbs(p) {
		return ""
	}
	cleaned := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if cleaned == "/" {
		return ""
	}
	return cleaned
}

// looksLikeWindowsAbs 识别 C:/Users/... 这类绝对路径。
func looksLikeWindowsAbs(p string) bool {
	if len(p) < 3 {
		return false
	}
	c := p[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	return p[1] == ':' && (p[2] == '/' || p[2] == '\\')
}

// isUnderHome 判断项目路径是否位于用户目录之下（需求 23.4）。
//
// 服务端不知道各机器的 home 具体值，所以只做形状判断：
// 必须是绝对路径、至少两级（/<home>/<project>），且不含上跳。
// 代理会在此之上再用真实的 home 做一次校验。
func isUnderHome(p string) bool {
	if looksLikeWindowsAbs(p) {
		parts := strings.Split(strings.Trim(p[2:], "/"), "/")
		return len(parts) >= 2 && !hasDotDot(parts)
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	return len(parts) >= 2 && !hasDotDot(parts)
}

func hasDotDot(parts []string) bool {
	for _, s := range parts {
		if s == ".." || s == "." || s == "" {
			return true
		}
	}
	return false
}

// dedupStrings 去重并排序，保证提交内容稳定。
func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// newProjectID 生成稳定的项目标识。
//
// 用“设备 + 路径”的内容哈希做种子，而不是随机值：
// 同一条登记反复提交时 ID 不变，避免下游状态里出现一堆同义 ID。
func newProjectID(deviceID, projectPath string) string {
	seed := deviceID + "\x00" + projectPath
	sum := sha256.Sum256([]byte(seed))
	return "prj-" + hex.EncodeToString(sum[:8])
}

// randomID 生成一个短的随机标识，用于覆盖事件。
func randomID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
