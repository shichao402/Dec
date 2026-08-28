package secrets

import (
	"fmt"
	"path"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

// Bitwarden 的 folder 只有一层：名字里的斜杠不建立层级。Dec 因此只用 P 名做
// folder，把平面与同步根相对路径一起编码进条目名（private/<plane>/<rel>）。
//
// 这套切分是本文件的唯一定义处：APIClient 之外的代码只见 (P, 平面, 相对路径)，
// 不得自行拼装 folder 名或条目名。
const bwPrivateSegment = "private"

// RemoteScope 是远端一个可寻址域：某个 P 的某个平面。零值非法。
type RemoteScope struct {
	P     string
	Plane SyncPlane
}

// NewRemoteScope 校验 P 名并归一化平面（user 与 machine 同义）。
func NewRemoteScope(pName string, plane SyncPlane) (RemoteScope, error) {
	name := strings.TrimSpace(pName)
	if !types.IsValidPName(name) {
		return RemoteScope{}, fmt.Errorf("P 名 %q 非法，必须为小写 kebab-case", pName)
	}
	switch {
	case IsMachinePlane(plane):
		return RemoteScope{P: name, Plane: SyncPlaneMachine}, nil
	case plane == SyncPlaneProject || plane == "":
		return RemoteScope{P: name, Plane: SyncPlaneProject}, nil
	default:
		return RemoteScope{}, fmt.Errorf("未知 SyncPlane %q", plane)
	}
}

// RemoteScopeOf 从 SyncTarget 取出远端寻址域。
func RemoteScopeOf(target SyncTarget) (RemoteScope, error) {
	if scope, err := ParseRemoteScope(target.Address); err == nil {
		return scope, nil
	}
	return NewRemoteScope(target.Name, target.Plane)
}

// String 返回逻辑地址 <p>/private/<plane>。它是展示、日志与配置里的稳定写法，
// 不是 Bitwarden folder 名。
func (s RemoteScope) String() string {
	if strings.TrimSpace(s.P) == "" {
		return ""
	}
	return path.Join(s.P, bwPrivateSegment, s.planeSegment())
}

// Valid 报告 scope 是否可用于远端读写。
func (s RemoteScope) Valid() bool {
	return types.IsValidPName(strings.TrimSpace(s.P)) && s.planeSegment() != ""
}

// ParseRemoteScope 解析逻辑地址 <p>/private/<plane>。它只接受 RemoteScope.String()
// 的产物，用于反序列化历史配置与跨进程传输，不接受裸 folder 名。
func ParseRemoteScope(label string) (RemoteScope, error) {
	clean := strings.Trim(strings.TrimSpace(strings.ReplaceAll(label, "\\", "/")), "/")
	parts := strings.Split(clean, "/")
	if len(parts) != 3 || parts[1] != bwPrivateSegment {
		return RemoteScope{}, fmt.Errorf("远端地址 %q 不是 <p>/%s/<plane>", label, bwPrivateSegment)
	}
	switch parts[2] {
	case string(SyncPlaneUser), string(SyncPlaneMachine):
		return NewRemoteScope(parts[0], SyncPlaneMachine)
	case string(SyncPlaneProject):
		return NewRemoteScope(parts[0], SyncPlaneProject)
	default:
		return RemoteScope{}, fmt.Errorf("远端地址 %q 的平面段非法", label)
	}
}

// planeSegment 返回条目名里的平面段：user 或 project。
func (s RemoteScope) planeSegment() string {
	if IsMachinePlane(s.Plane) {
		return string(SyncPlaneUser)
	}
	if s.Plane == SyncPlaneProject || s.Plane == "" {
		return string(SyncPlaneProject)
	}
	return ""
}

// folderName 返回 Bitwarden 上真实的 folder 名：只有 P 名这一级。
func (s RemoteScope) folderName() string {
	return strings.TrimSpace(s.P)
}

// itemPrefix 返回该平面全部条目名共有的前缀（含结尾斜杠）。
func (s RemoteScope) itemPrefix() string {
	return bwPrivateSegment + "/" + s.planeSegment() + "/"
}

// encodeItemName 把同步根相对路径转成 Bitwarden 条目名。
// Secure Note 与 SSH Key Item 共用同一编码：folder 合并到 P 之后，平面只能靠
// 条目名区分，SSH Key 也不能例外。
func (s RemoteScope) encodeItemName(rel string) (string, error) {
	if !s.Valid() {
		return "", fmt.Errorf("远端寻址域非法: P=%q plane=%q", s.P, s.Plane)
	}
	clean, err := normalizeSyncRelPath(rel)
	if err != nil {
		return "", err
	}
	return s.itemPrefix() + clean, nil
}

// decodeItemName 从 Bitwarden 条目名还原同步根相对路径。
// 前缀不匹配（属于另一平面，或不是 Dec 写入的条目）时返回 ok=false，让调用方
// 跳过而不是把别人的条目当成本平面资产。
func (s RemoteScope) decodeItemName(itemName string) (string, bool) {
	if !s.Valid() {
		return "", false
	}
	trimmed := strings.TrimSpace(strings.ReplaceAll(itemName, "\\", "/"))
	prefix := s.itemPrefix()
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	rel, err := normalizeSyncRelPath(strings.TrimPrefix(trimmed, prefix))
	if err != nil {
		return "", false
	}
	return rel, true
}

// bwScope 是一次远端读写的实际落点：Bitwarden folder 名 + 条目名编解码规则。
// P 布局按 private/<plane>/ 前缀编解码；存量非 P folder 的条目名保持原样。
type bwScope struct {
	folder string
	scope  RemoteScope
	isP    bool
}

// bwScopeFromFolderName 解析调用方给出的 folder 名。
// 逻辑地址 <p>/private/<plane> 与裸 P 名都落到 P 布局；其余按存量 folder 处理。
func bwScopeFromFolderName(name string) bwScope {
	trimmed := strings.TrimSpace(name)
	if scope, err := ParseRemoteScope(trimmed); err == nil {
		return bwScope{folder: scope.folderName(), scope: scope, isP: true}
	}
	return bwScope{folder: trimmed}
}

func bwScopeOf(scope RemoteScope) bwScope {
	return bwScope{folder: scope.folderName(), scope: scope, isP: true}
}

// bwScopeForTarget 返回 target 的远端落点。声明型 P target 走扁平映射；只读浏览
// 节点按存量 folder 名直连，让 Remote 页仍能看见并清理非 P 遗留。
func bwScopeForTarget(t SyncTarget) (bwScope, error) {
	if addr := strings.TrimSpace(t.Address); addr != "" {
		return bwScopeFromFolderName(addr), nil
	}
	scope, err := NewRemoteScope(t.Name, t.Plane)
	if err != nil {
		return bwScope{}, err
	}
	return bwScopeOf(scope), nil
}

// encode 把同步根相对路径转成该 folder 内的条目名。
func (s bwScope) encode(rel string) (string, error) {
	if s.isP {
		return s.scope.encodeItemName(rel)
	}
	return normalizeSyncRelPath(rel)
}

// decode 把条目名还原为同步根相对路径；不属于本 scope 时返回 ok=false。
func (s bwScope) decode(itemName string) (string, bool) {
	if s.isP {
		return s.scope.decodeItemName(itemName)
	}
	trimmed := strings.TrimSpace(itemName)
	if trimmed == "" {
		return "", false
	}
	// 存量 folder 的条目名不带平面前缀；带前缀的说明已迁到 P 布局，不属于这里。
	if _, isP := bwPlaneSegmentOfItemName(trimmed); isP {
		return "", false
	}
	return trimmed, true
}

// bwPlaneSegmentOfItemName 从条目名前缀识别平面，用于按 folder 反查存在哪些平面。
func bwPlaneSegmentOfItemName(itemName string) (SyncPlane, bool) {
	trimmed := strings.Trim(strings.TrimSpace(strings.ReplaceAll(itemName, "\\", "/")), "/")
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) != 3 || parts[0] != bwPrivateSegment {
		return "", false
	}
	switch parts[1] {
	case string(SyncPlaneUser):
		return SyncPlaneMachine, true
	case string(SyncPlaneProject):
		return SyncPlaneProject, true
	default:
		return "", false
	}
}
