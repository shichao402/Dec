package app

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

// DeleteTypedConfirmSpec 描述跨上下文删除时的 typed confirm 要求。
type DeleteTypedConfirmSpec struct {
	Required bool
	// Expect 用户必须输入的短语：单一风险 folder 名，或 "DELETE"。
	// 当 Expect 为具体 folder 时，输入 "DELETE" 也视为通过。
	Expect string
	Reason string
}

// AnalyzeDeleteTypedConfirm 判断选中项是否需要真正输入确认。
// 触发条件：Unmanaged（其它项目裸 folder / 非 Dec 管理）、另一平面标注、或摘要标明的跨上下文风险。
// 同上下文 / 本地清理保持低风险，Required=false。
func AnalyzeDeleteTypedConfirm(items []DeleteSelectionItem, workspace Workspace) DeleteTypedConfirmSpec {
	if len(items) == 0 {
		return DeleteTypedConfirmSpec{}
	}
	riskFolders := make([]string, 0)
	seenFolder := make(map[string]struct{})
	var reasons []string
	seenReason := make(map[string]struct{})
	addReason := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" {
			return
		}
		if _, ok := seenReason[r]; ok {
			return
		}
		seenReason[r] = struct{}{}
		reasons = append(reasons, r)
	}
	addFolder := func(folder string) {
		folder = strings.TrimSpace(folder)
		if folder == "" {
			return
		}
		if _, ok := seenFolder[folder]; ok {
			return
		}
		seenFolder[folder] = struct{}{}
		riskFolders = append(riskFolders, folder)
	}

	plane := workspace.EffectivePlane()
	for _, item := range items {
		if item.Partition == PartitionLocal {
			continue
		}
		if item.Unmanaged {
			addReason("含非 Dec 管理 / 其它项目裸 folder")
			addFolder(item.SecretsBundle)
			continue
		}
		if crossPlaneScope(item.ScopeTag, plane) {
			addReason(fmt.Sprintf("含另一平面项（scope:%s）", item.ScopeTag))
			if item.SecretsBundle != "" {
				addFolder(item.SecretsBundle)
			} else if item.BundleName != "" {
				addFolder(item.BundleName)
			} else if item.Vault != "" {
				addFolder(item.Vault)
			}
			continue
		}
		if crossPlaneSecrets(item.Plane, plane, item.Kind) {
			addReason("含另一平面 secrets 落地项")
			addFolder(item.SecretsBundle)
		}
	}
	if len(reasons) == 0 {
		return DeleteTypedConfirmSpec{}
	}
	expect := "DELETE"
	if len(riskFolders) == 1 {
		expect = riskFolders[0]
	}
	return DeleteTypedConfirmSpec{
		Required: true,
		Expect:   expect,
		Reason:   strings.Join(reasons, "；"),
	}
}

func crossPlaneScope(scopeTag string, plane WorkspacePlane) bool {
	scopeTag = strings.TrimSpace(scopeTag)
	if scopeTag == "" {
		return false
	}
	if plane == WorkspaceUser {
		return scopeTag == "project"
	}
	return scopeTag == "user"
}

func crossPlaneSecrets(itemPlane secrets.SyncPlane, workspacePlane WorkspacePlane, kind DeleteItemKind) bool {
	if kind != DeleteKindSecret && kind != DeleteKindSSHKey {
		return false
	}
	if itemPlane == "" {
		return false
	}
	if workspacePlane == WorkspaceUser {
		return !secrets.IsMachinePlane(itemPlane)
	}
	return secrets.IsMachinePlane(itemPlane)
}

// MatchDeleteTypedConfirm 校验用户输入是否满足 typed confirm。
// Expect 为具体 folder 时，输入该 folder 或 "DELETE"（大小写敏感对 DELETE，folder 精确匹配）均通过。
func MatchDeleteTypedConfirm(input string, spec DeleteTypedConfirmSpec) bool {
	if !spec.Required {
		return true
	}
	got := strings.TrimSpace(input)
	expect := strings.TrimSpace(spec.Expect)
	if got == "" || expect == "" {
		return false
	}
	if got == "DELETE" {
		return true
	}
	return got == expect
}
