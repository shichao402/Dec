package app

import (
	"context"
	"strings"

	"github.com/shichao402/Dec/internal/install"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// BelongingInfo 是一个产品名的归属判定结果（ADR 0034）。零值表示「未知/不可达」，
// 不表示「无归属」——无归属用 Orphan=true 表达。
type BelongingInfo struct {
	OriginRepo   string
	IdentityOnly bool
	Orphan       bool
	// DeclaredPlane 是该产品声明的密钥平面（ADR 0035）："global" | "local" | ""（未声明）。
	DeclaredPlane string
}

// secretsBelongingSnapshotSource 读 registry head 快照；包级变量供测试注入，
// 避免测试打真实 registry。
var secretsBelongingSnapshotSource = officialHeadSnapshots

// secretsAddressProject 从远端地址提取产品名。逻辑地址 <p>/private/<plane> 取 P；
// 裸 kebab-case 名视为产品名 folder；其余（"Dec"、"bundle/x" 等存量 folder）不是产品，
// 不参与归属判定，维持既有的 Unmanaged 语义。
func secretsAddressProject(address string) (string, bool) {
	address = strings.TrimSpace(address)
	if scope, err := secrets.ParseRemoteScope(address); err == nil {
		return scope.P, true
	}
	if types.IsValidPName(address) {
		return address, true
	}
	return "", false
}

// secretsBelongingResolver 为一批产品名做归属判定（ADR 0034）。
//
// 数据源优先级：registry head 快照 > 本工作区作者声明 > 本机 cache 已装回落。
// 归属是全局事实：项目平面调用同样读 registry（ADR 0034 平面边界）。
// registry 不可达时（快照为 nil 或空）只做无网络依赖的 cache 回落，Orphan 一律
// false——绝不把「连不上」误判成「孤儿」。
type secretsBelongingResolver struct {
	reachable bool
	snapshots map[string]registry.ProjectSnapshot
	cacheDir  string
	// authors 是本工作区的作者声明（ProjectName + Products key）→ origin repo。
	// 作者侧产品尚未发布时 registry 查无，但不是孤儿。
	authors map[string]string
	// authorPlanes 是本工作区作者声明的密钥平面（ADR 0035），作者侧产品判定用。
	authorPlanes map[string]string
	// consumed 是本工作区 requires 消费的产品名，用于删除候选的高置信判定。
	consumed map[string]struct{}
	memo     map[string]BelongingInfo
}

func newSecretsBelongingResolver(ctx context.Context, workspace Workspace, cfg *types.ProjectConfig) *secretsBelongingResolver {
	_, url := workspaceOfficialRequires(workspace, cfg)
	snapshots := secretsBelongingSnapshotSource(ctx, url)
	authors := make(map[string]string)
	authorPlanes := make(map[string]string)
	if cfg != nil {
		origin := strings.TrimSpace(cfg.OriginRepo)
		if home := homeProjectName(workspace, cfg); home != "" {
			authors[home] = origin
			authorPlanes[home] = string(cfg.SecretsPlane)
		}
		for name, decl := range cfg.Products {
			authors[name] = origin
			authorPlanes[name] = string(decl.SecretsPlane)
		}
	}
	consumed := make(map[string]struct{})
	for _, name := range consumedProjectNames(cfg, workspace.EffectivePlane()) {
		consumed[name] = struct{}{}
	}
	return &secretsBelongingResolver{
		// 空 map 与不可达无法区分；registry 不会真的空，保守按不可达处理，
		// 宁可漏报孤儿也不误报。
		reachable:    len(snapshots) > 0,
		snapshots:    snapshots,
		cacheDir:     workspaceCacheDir(workspace),
		authors:      authors,
		authorPlanes: authorPlanes,
		consumed:     consumed,
		memo:         make(map[string]BelongingInfo),
	}
}

// resolve 返回产品名的归属判定。registry 查无且没有任何本地证据时 Orphan=true；
// requires 消费不参与行级判定（被 yank 的订阅确实该提示无主）。
func (r *secretsBelongingResolver) resolve(p string) BelongingInfo {
	p = strings.TrimSpace(p)
	if p == "" {
		return BelongingInfo{}
	}
	if info, ok := r.memo[p]; ok {
		return info
	}
	info := r.compute(p)
	r.memo[p] = info
	return info
}

func (r *secretsBelongingResolver) compute(p string) BelongingInfo {
	if !r.reachable {
		return BelongingInfo{OriginRepo: install.ReadOriginRepo(r.cacheDir, p), DeclaredPlane: install.ReadSecretsPlane(r.cacheDir, p)}
	}
	if snap, ok := r.snapshots[p]; ok {
		origin := strings.TrimSpace(snap.OriginRepo)
		if origin == "" {
			origin = install.ReadOriginRepo(r.cacheDir, p)
		}
		return BelongingInfo{
			OriginRepo:    origin,
			IdentityOnly:  len(snap.Assets) == 0,
			DeclaredPlane: strings.TrimSpace(snap.SecretsPlane),
		}
	}
	if origin, ok := r.authors[p]; ok {
		// 作者侧产品：平面以本工作区作者声明为准（尚未发布时 registry 查无）。
		return BelongingInfo{OriginRepo: origin, DeclaredPlane: r.authorPlanes[p]}
	}
	if origin := install.ReadOriginRepo(r.cacheDir, p); origin != "" {
		// 已安装但不在 registry head：本地证据说明它有主（可能被撤或尚未同步）。
		return BelongingInfo{OriginRepo: origin, DeclaredPlane: install.ReadSecretsPlane(r.cacheDir, p)}
	}
	return BelongingInfo{Orphan: true}
}

// highConfidenceOrphan 报告「registry 查无 + 本工作区无 requires 消费」的 folder，
// 是 ADR 0034 定义的高置信删除候选。
func (r *secretsBelongingResolver) highConfidenceOrphan(p string) bool {
	if _, consumed := r.consumed[strings.TrimSpace(p)]; consumed {
		return false
	}
	return r.resolve(p).Orphan
}

// belongingAnnotations 给删除候选做一次归属标注。只在远端分区生效：
// folder 归属是远端事实，本地分区只是落地文件残留。
func belongingAnnotations(resolver *secretsBelongingResolver, secretsBundle string, partition RemotePartition) (origin string, identityOnly, highConfidence bool) {
	if resolver == nil || partition != PartitionRemote {
		return "", false, false
	}
	p, ok := secretsAddressProject(secretsBundle)
	if !ok {
		return "", false, false
	}
	info := resolver.resolve(p)
	return info.OriginRepo, info.IdentityOnly, resolver.highConfidenceOrphan(p)
}

// belongingRowAnnotation 是密钥清单行的归属标注（ADR 0034）：
// 远端地址 → 产品名 → registry 判定；非产品 folder 不参与归属。
func belongingRowAnnotation(resolver *secretsBelongingResolver, address string) (origin string, identityOnly, orphan bool) {
	if resolver == nil {
		return "", false, false
	}
	p, ok := secretsAddressProject(address)
	if !ok {
		return "", false, false
	}
	info := resolver.resolve(p)
	return info.OriginRepo, info.IdentityOnly, info.Orphan
}

// declaredPlaneOf 返回产品声明的密钥平面（ADR 0035）："global" | "local" | ""。
// 未声明产品返回空串；非产品名同样返回空串（不参与平面判定）。
func declaredPlaneOf(resolver *secretsBelongingResolver, p string) string {
	if resolver == nil {
		return ""
	}
	return resolver.resolve(strings.TrimSpace(p)).DeclaredPlane
}

// assetPlaneMatchesWorkspace 报告产品声明平面与 workspace 平面是否同侧。
// 声明 global 只在本机（用户）平面生效；声明 local 只在项目平面生效。
// 未声明（空）恒为 true——迁移期 fail-open，行为与声明前一致。
func assetPlaneMatchesWorkspace(declared string, workspace Workspace) bool {
	switch strings.TrimSpace(declared) {
	case "":
		return true
	case string(types.AssetPlaneGlobal):
		return workspace.EffectivePlane() == WorkspaceGlobal
	case string(types.AssetPlaneLocal):
		return workspace.EffectivePlane() == WorkspaceLocal
	default:
		// 声明取值异常：不因此阻断同步，按未声明处理。
		return true
	}
}
