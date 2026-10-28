package render

import (
	"fmt"
	"sort"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// file 资产类型的渲染细节（.agent/credential-landing 需求 4、5）。

// shapeFileItem 按条目声明调整下发描述：格式、权限与内容规范化。
//
// 规范化必须与代理落盘时一致（proto.NormalizeSSHKey / NormalizeBlockBody），
// 否则服务端算的哈希与代理读回的哈希永远对不上，每轮都会重发。
func shapeFileItem(pf *proto.File, item ssot.ManifestItem) error {
	switch item.Format {
	case "", ssot.FileFormatWhole:
		pf.Format = proto.FormatWhole
	case ssot.FileFormatManagedBlock:
		pf.Format = proto.FormatManagedBlock
	default:
		return fmt.Errorf("file 条目 %q 的格式 %q 不受支持", item.Path, item.Format)
	}
	if !pf.Secret && item.Mode != 0 {
		pf.Mode = item.Mode
	}
	if item.Root == proto.RootSSH && proto.IsSSHPrivateKey(item.Target) {
		pf.Content = proto.NormalizeSSHKey(pf.Content)
	}
	if pf.Format == proto.FormatManagedBlock {
		body, ok := proto.NormalizeBlockBody(pf.Content)
		if !ok {
			return fmt.Errorf("file 条目 %q 的内容含有托管块边界标记", item.Path)
		}
		pf.Content = body
	}
	return nil
}

// pathSlot 是一个落点在设备上的唯一位置。
func pathSlot(t proto.Target) string {
	return string(t.Plan) + "|" + t.ProjectID + "|" + t.Root + "|" + t.Path
}

// firstSlotConflict 返回第一个已被别的条目占用的落点描述，没有冲突时返回空。
func firstSlotConflict(seen map[string]string, pf proto.File) string {
	for _, t := range pf.Targets {
		if prev, dup := seen[pathSlot(t)]; dup && prev != pf.ID {
			return fmt.Sprintf("落点冲突：%s/%s 已由 %s 占用", t.Root, t.Path, prev)
		}
	}
	return ""
}

// isCredentialRoot 报告落点根是否是凭据根。
func isCredentialRoot(root string) bool {
	return root == proto.RootSSH || root == proto.RootSecrets
}

// credentialRootsOf 返回下发描述用到的凭据根。
func credentialRootsOf(f proto.File) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range f.Targets {
		if isCredentialRoot(t.Root) && !seen[t.Root] {
			seen[t.Root] = true
			out = append(out, t.Root)
		}
	}
	sort.Strings(out)
	return out
}

// ReasonAgentTooOld 是设备代理不支持凭据根时的告警原因（需求 4.5）。
const ReasonAgentTooOld = "代理版本过低，不支持该落点根"

// filterRoots 把设备代理没有上报的凭据根条目从期望状态里拿掉，改记为告警。
//
// 改记为告警而不是直接丢弃，是为了借用「渲染失败的条目不删除」这条既有规则
// （Sync 里的 failed 集合）：老代理上即便以前有过同 ID 的落点，也不会被误删。
func filterRoots(want *Rendered, roots []string) *Rendered {
	supported := make(map[string]bool, len(roots))
	for _, r := range roots {
		supported[r] = true
	}
	out := &Rendered{Revision: want.Revision, Alerts: append([]Alert(nil), want.Alerts...)}
	for _, f := range want.Files {
		missing := ""
		for _, r := range credentialRootsOf(f.File) {
			if !supported[r] {
				missing = r
				break
			}
		}
		if missing != "" {
			out.Alerts = append(out.Alerts, Alert{ID: f.File.ID, Reason: ReasonAgentTooOld + "：" + missing})
			continue
		}
		out.Files = append(out.Files, f)
	}
	sort.Slice(out.Alerts, func(i, j int) bool { return out.Alerts[i].ID < out.Alerts[j].ID })
	return out
}

// RenderFor 渲染一台设备、并按其代理上报的落点根过滤后的期望状态。
func (r *Renderer) RenderFor(dev ssot.Device, roots []string) (*Rendered, error) {
	want, err := r.RenderDevice(dev)
	if err != nil {
		return nil, err
	}
	return filterRoots(want, roots), nil
}

// RenderReported 按设备最近一次上报的落点根渲染，供管理面计算漂移。
func (r *Renderer) RenderReported(dev ssot.Device) (*Rendered, error) {
	roots, err := r.states.Roots(dev.ID)
	if err != nil {
		return nil, err
	}
	return r.RenderFor(dev, roots)
}

// TooOld 返回因代理不支持落点根而没有下发的条目数（需求 4.5）。
func (r *Rendered) TooOld() int {
	n := 0
	for _, a := range r.Alerts {
		if len(a.Reason) >= len(ReasonAgentTooOld) && a.Reason[:len(ReasonAgentTooOld)] == ReasonAgentTooOld {
			n++
		}
	}
	return n
}

// RecordRoots 记下代理上报的落点根。没有变化时不写盘：每次长轮询都会走到这里。
func (s *StateStore) RecordRoots(deviceID string, roots []string) error {
	norm := append([]string(nil), roots...)
	sort.Strings(norm)
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadLocked(deviceID)
	if err != nil {
		return err
	}
	if equalStrings(st.Roots, norm) {
		return nil
	}
	st.Roots = norm
	return s.saveLocked(st)
}

// Roots 返回代理最近一次上报的落点根。
func (s *StateStore) Roots(deviceID string) ([]string, error) {
	st, err := s.Load(deviceID)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), st.Roots...), nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
