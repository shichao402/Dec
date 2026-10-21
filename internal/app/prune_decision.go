package app

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

// 剪枝分类。判定结果只有三类，下游据此决定「移入隔离区」还是「保留并说明」。
const (
	// PruneClassRebuildable 表示本地内容可由远端状态完整还原，剪掉不会丢信息。
	PruneClassRebuildable = "rebuildable"
	// PruneClassLocalOnly 表示本地存在远端没有的内容，删了就找不回来。
	PruneClassLocalOnly = "local_only"
	// PruneClassUnknown 表示无法判断来源，按保留处理。
	PruneClassUnknown = "unknown"
)

// PruneItem 是一条本地条目的剪枝判定结果。
//
// 这是预览与执行共用的唯一判定单元：预览只读它，执行按它动手。两边读同一份
// 结论，才不会出现「预览说保留、执行照样删」。
type PruneItem struct {
	// Class 是分类：rebuildable / local_only / unknown。
	Class string
	// Kind 是条目类型：note / ssh_key / dir。
	Kind string
	// AbsPath 是绝对路径，执行阶段据此移入隔离区。
	AbsPath string
	// DisplayPath 是给人看的路径。
	DisplayPath string
	// RelPath 是相对同步根的路径。
	RelPath string
	// Bundle 是所属项目或 bundle 名。
	Bundle string
	// Plane 是来源平面。
	Plane string
	// ProjectRoot 是项目根，user 平面为空。
	ProjectRoot string
	// Reason 是剪枝理由（仅 rebuildable 有值）。
	Reason prune.Reason
	// Detail 说明命中了哪条规则，预览与事件里都会展示。
	Detail string
	// RemoteRecoverable 表示该条目即使被剪掉，也能从远端重新拉回。
	RemoteRecoverable bool
	// IsDir 标记整个目录。
	IsDir bool
}

// PruneDecision 是一次剪枝判定的完整结果。
type PruneDecision struct {
	// Target 是该判定对应的同步目标。
	Target secrets.SyncTarget
	// Items 是全部判定项，含保留项。
	Items []PruneItem
	// Warnings 是扫描过程中的告警，调用方应转成事件。
	Warnings []string
}

// Removable 返回归类为可重建、可以移入隔离区的条目。
func (d PruneDecision) Removable() []PruneItem {
	out := make([]PruneItem, 0, len(d.Items))
	for _, item := range d.Items {
		if item.Class == PruneClassRebuildable {
			out = append(out, item)
		}
	}
	return out
}

// Kept 返回必须保留的条目。
func (d PruneDecision) Kept() []PruneItem {
	out := make([]PruneItem, 0, len(d.Items))
	for _, item := range d.Items {
		if item.Class != PruneClassRebuildable {
			out = append(out, item)
		}
	}
	return out
}

// PruneScope 限定一次判定的范围，供预览做局部计算。
type PruneScope struct {
	// Bundle 非空时只判定该 bundle。
	Bundle string
	// PathPrefix 非空时只判定显示路径以此开头的条目。
	PathPrefix string
	// Strict 表示用户显式要求「本地与远端严格一致」（需求 8.2）：
	// 对默认保护（未确认来源 / 非派生类型）的条目也执行隔离，
	// 从而清空本地残留。仍走隔离区软删，绝不硬删。
	Strict bool
}

func (s PruneScope) match(item PruneItem) bool {
	if s.Bundle != "" && !strings.EqualFold(item.Bundle, s.Bundle) {
		return false
	}
	if s.PathPrefix != "" && !strings.HasPrefix(filepath.ToSlash(item.DisplayPath), filepath.ToSlash(s.PathPrefix)) {
		return false
	}
	return true
}

// DecideTargetPrune 针对一个已成功对照远端的 SyncTarget，给出本地条目的剪枝判定。
//
// 只读：不移动、不删除任何文件。remoteNotes / remoteKeys 必须是本轮真实取回的
// 远端列表；取回失败时调用方不得调用本函数（无从判断孤儿）。
func DecideTargetPrune(
	projectRoot string,
	target secrets.SyncTarget,
	remoteNotes []secrets.SecureNote,
	remoteKeys []secrets.SSHKeyLanding,
	scope PruneScope,
) PruneDecision {
	decision := PruneDecision{Target: target}

	keepNotes := make(map[string]secrets.SecureNote, len(remoteNotes))
	for _, note := range remoteNotes {
		rel := strings.TrimSpace(note.RelativePath)
		if rel == "" {
			continue
		}
		keepNotes[rel] = note
	}

	localNotes, scanErr := secrets.ScanSyncRoot(projectRoot, target)
	if scanErr != nil {
		decision.Warnings = append(decision.Warnings,
			fmt.Sprintf("扫描本地 %s 失败（跳过 secrets 剪枝）: %v", target.LocalRoot, scanErr))
		return decision
	}
	for _, note := range localNotes {
		if _, ok := keepNotes[note.RelativePath]; ok {
			continue
		}
		item := decideNote(projectRoot, target, note, scope)
		if !scope.match(item) {
			continue
		}
		decision.Items = append(decision.Items, item)
	}

	for _, item := range decideSSHKeys(projectRoot, target, remoteKeys, scope) {
		decision.Items = append(decision.Items, item)
	}

	sort.Slice(decision.Items, func(i, j int) bool {
		if decision.Items[i].Bundle != decision.Items[j].Bundle {
			return decision.Items[i].Bundle < decision.Items[j].Bundle
		}
		if decision.Items[i].Kind != decision.Items[j].Kind {
			return decision.Items[i].Kind < decision.Items[j].Kind
		}
		return decision.Items[i].DisplayPath < decision.Items[j].DisplayPath
	})
	return decision
}

// decideNote 判定一条远端已无的本地 Secure Note。
func decideNote(projectRoot string, target secrets.SyncTarget, note secrets.SecureNote, scope PruneScope) PruneItem {
	item := PruneItem{
		Kind:        "note",
		RelPath:     note.RelativePath,
		Bundle:      target.Name,
		Plane:       string(target.Plane),
		ProjectRoot: projectRoot,
	}
	if secrets.IsMachinePlane(target.Plane) {
		item.ProjectRoot = ""
	}

	display, err := secrets.RootRelPath(target, note.RelativePath)
	if err != nil {
		item.Class = PruneClassUnknown
		item.Detail = "解析展示路径失败，按保留处理"
		return item
	}
	item.DisplayPath = display

	abs, err := secrets.AbsolutePath(projectRoot, target, note.RelativePath)
	if err != nil {
		item.Class = PruneClassUnknown
		item.Detail = "解析绝对路径失败，按保留处理"
		return item
	}
	item.AbsPath = abs

	// 只删同步根内的落点。越界一律保留——这个判断以前只在执行阶段做，
	// 现在提前到判定阶段，预览与执行共用。
	if escaped, why := escapesSyncRoot(projectRoot, target, abs); escaped {
		item.Class = PruneClassUnknown
		item.Detail = why
		return item
	}

	// 远端已无对应条目。这里的关键在于：不能因为「远端没有」就推「本地可删」。
	// 这条等式只在「本地内容全部来自远端」时才成立。判定依据是「内容类型是否
	// 由远端生成」：env 注入源与 handler 托管条目（.gcm 等）都是远端 Note 派生
	// 的产物，本地不承载独立语义，删掉可靠远端重建；其余类型（files/ 下的私钥
	// 材料等）可能被手工写入，一律保留。
	if !isDerivedFromRemote(note.RelativePath) {
		if scope.Strict {
			item.Class = PruneClassRebuildable
			item.Reason = prune.ReasonRemoteGone
			item.RemoteRecoverable = false
			item.Detail = "strict：远端已无该条目，按严格一致要求移入隔离区（本地可能含手工内容，仍可恢复）"
			return item
		}
		item.Class = PruneClassLocalOnly
		item.Detail = "远端已无该条目，但本地内容不属于远端派生类型，可能含手工维护内容"
		return item
	}

	item.Class = PruneClassRebuildable
	item.Reason = prune.ReasonRemoteGone
	item.RemoteRecoverable = true
	item.Detail = "远端已无该条目，本地为远端派生结果，可重建"
	return item
}

// decideSSHKeys 判定远端已无的本地 SSH Key 落地。
func decideSSHKeys(projectRoot string, target secrets.SyncTarget, remoteKeys []secrets.SSHKeyLanding, scope PruneScope) []PruneItem {
	owner := secretsOwnerForTarget(target)
	keep := make(map[string]struct{}, len(remoteKeys))
	for _, key := range remoteKeys {
		name := strings.TrimSpace(key.Name)
		if name == "" {
			continue
		}
		keep[name] = struct{}{}
	}

	var localNames []string
	if secrets.IsMachinePlane(target.Plane) {
		localNames = listLocalSSHKeyNamesForBundle(owner)
	} else {
		names, err := secrets.ListProjectSSHKeyNames(projectRoot, owner)
		if err != nil {
			return []PruneItem{{
				Class:       PruneClassUnknown,
				Kind:        "ssh_key",
				Bundle:      target.Name,
				Plane:       string(target.Plane),
				ProjectRoot: projectRoot,
				Detail:      fmt.Sprintf("列出 project SSH Key 失败，按保留处理: %v", err),
			}}
		}
		localNames = names
	}

	out := make([]PruneItem, 0, len(localNames))
	for _, keyName := range localNames {
		if _, ok := keep[keyName]; ok {
			continue
		}
		item := PruneItem{
			Kind:              "ssh_key",
			RelPath:           keyName,
			Bundle:            target.Name,
			Plane:             string(target.Plane),
			ProjectRoot:       projectRoot,
			Class:             PruneClassRebuildable,
			Reason:            prune.ReasonSSHLanding,
			RemoteRecoverable: true,
			Detail:            "远端已无该 SSH Key，本地为 Dec 托管落地，可由远端重新落地",
		}
		if secrets.IsMachinePlane(target.Plane) {
			item.ProjectRoot = ""
			privPath, nameErr := secrets.MachineSSHKeyPrivatePath(owner, keyName)
			if nameErr != nil {
				item.Class = PruneClassUnknown
				item.Detail = "解析 SSH Key 落地路径失败，按保留处理"
				if !scope.match(item) {
					continue
				}
				out = append(out, item)
				continue
			}
			item.DisplayPath = "~/.ssh/" + filepath.Base(privPath)
			item.AbsPath = privPath
		} else {
			item.DisplayPath = owner + "/" + keyName
			privPath, nameErr := secrets.ProjectSSHKeyPrivatePath(projectRoot, owner, keyName)
			if nameErr != nil {
				item.Class = PruneClassUnknown
				item.Detail = "解析 project SSH Key 落地路径失败，按保留处理"
				if !scope.match(item) {
					continue
				}
				out = append(out, item)
				continue
			}
			item.AbsPath = privPath
		}
		if !scope.match(item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// decideMissingBundleDirs 判定「本次启用、但 vault 已无目录」的包留下的本地 secrets 目录。
//
// bundle 目录由 pull 从远端派生，整体可重建；因此远端已确认时允许移入隔离区。
// 未确认远端时只报告不删——这一点与执行阶段的历史行为保持一致。
func decideMissingBundleDirs(workspace Workspace, bundleName string, remoteConfirmed bool) []PruneItem {
	out := make([]PruneItem, 0, 2)
	for _, dir := range localSecretBundleDirsForPlane(workspace, bundleName) {
		info, statErr := os.Stat(dir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		item := PruneItem{
			Kind:        "dir",
			AbsPath:     dir,
			DisplayPath: dir,
			RelPath:     filepath.Base(dir),
			Bundle:      bundleName,
			Plane:       string(workspace.EffectivePlane()),
			ProjectRoot: workspace.Root,
			IsDir:       true,
		}
		if workspace.EffectivePlane() == WorkspaceUser {
			item.ProjectRoot = ""
			item.DisplayPath = "~/.dec/secrets/" + filepath.Base(dir)
		}
		if !remoteConfirmed {
			item.Class = PruneClassUnknown
			item.Detail = "vault 已无该包，但本轮未能确认 Bitwarden，未做删除"
			out = append(out, item)
			continue
		}
		item.Class = PruneClassRebuildable
		item.Reason = prune.ReasonDerivedBundle
		item.Detail = "vault 与 Bitwarden 均已确认无该包，本地目录为派生落地"
		out = append(out, item)
	}
	return out
}

// isDerivedFromRemote 判断一条同步根内路径是否属于「完全由远端生成」的类型。
//
// secrets 的 note 类条目（env 注入源、GCM 等 handler 托管、普通 plain note）
// 内容都是远端 Secure Note 的逐字落盘，本地不承载独立语义，远端确认后删除
// 可靠重建。只有 files 等材料目录里放的东西（私钥、证书等散件）可能是手工
// 维护的，不视为可重建。
//
// 判定用「路径类型」而非仅文件名匹配：普通 note 用 ParseTypePath 识别为
// SecretTypePlain，handler 托管用 registry 匹配。files 目录里的文件会落到
// 非 note 的路径形态，从而被排除。
func isDerivedFromRemote(rel string) bool {
	clean := path.Clean(strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/"))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") {
		return false
	}
	tp, ok, err := secrets.ParseTypePath(clean)
	if err != nil {
		// 未知点目录（如 .files）解析失败硬返回，视为非 note 落盘，保守保留。
		return false
	}
	if ok {
		// 点类型目录：env 与 handler 托管（gcm）可重建。
		if tp.Type.ID == secrets.SecretTypeEnv {
			return true
		}
		if handler.Default().Find(handler.SourceNote, clean) != nil {
			return true
		}
		// 其它点类型（sshkey 等）不在此剪枝范围，保守保留。
		return false
	}
	// 普通 plain note：内容即远端 Note 正文的落盘，可重建。
	return true
}

// escapesSyncRoot 判断 abs 是否逃出同步根。
func escapesSyncRoot(projectRoot string, target secrets.SyncTarget, abs string) (bool, string) {
	rootAbs, err := secrets.ResolveAbsDir(projectRoot, target)
	if err != nil {
		return true, "解析同步根失败，按保留处理"
	}
	cleanAbs := filepath.Clean(abs)
	cleanRoot := filepath.Clean(rootAbs)
	rel, relErr := filepath.Rel(cleanRoot, cleanAbs)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true, "目标路径在同步根之外，按保留处理"
	}
	return false, ""
}

// pruneDecisionIsEmpty 判断判定结果里是否既无可删项也无保留项。
func pruneDecisionIsEmpty(d PruneDecision) bool {
	return len(d.Items) == 0
}

// summarizePruneDecision 给出一句可用于事件的结论。
func summarizePruneDecision(d PruneDecision) string {
	removable := len(d.Removable())
	kept := len(d.Kept())
	if removable == 0 && kept == 0 {
		return "无待剪枝条目"
	}
	if kept == 0 {
		return fmt.Sprintf("%d 项可剪枝", removable)
	}
	if removable == 0 {
		return fmt.Sprintf("无待剪枝条目（保留 %d 项）", kept)
	}
	return fmt.Sprintf("%d 项可剪枝，保留 %d 项", removable, kept)
}

// prunePathHint 把同步根相对路径转成用于匹配 PathPrefix 的展示形式。
func prunePathHint(target secrets.SyncTarget, rel string) string {
	display, err := secrets.RootRelPath(target, rel)
	if err != nil {
		return path.Clean(rel)
	}
	return display
}
