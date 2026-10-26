package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 设备下发状态（需求 4.2、4.3、5.2）。
//
// 状态只存在云端，代理不保存任何下发清单：对账时由服务端给出清单，
// 代理现场算哈希后上报。所以这里存的是“服务端认为设备上应该是什么样”，
// 以及“最后一次确认落地的哈希”。
//
// 状态不写进 git：每次回执都会变，进仓库只会产生噪声。

// fileState 是单个落点的状态。
type fileState struct {
	// ID 是条目标识。
	ID string `json:"id"`
	// Hash 是最后一次确认落地的内容哈希。
	Hash string `json:"hash"`
	// Paths 是代理回报的实际绝对路径，用于审计与吊销后的轮换清单。
	Paths []string `json:"paths,omitempty"`
	// Secret 标记内容含密钥。
	Secret bool `json:"secret,omitempty"`
	// Format 与 Key 记录下发形状，用于发现“文件被外部改成了别的形状”。
	Format proto.Format `json:"format,omitempty"`
	Key    string       `json:"key,omitempty"`
	// AppliedAt 是最后一次成功落地时间。
	AppliedAt time.Time `json:"applied_at"`
	// ProjectIDs 是这条文件涉及的设备内部目标（项目平面用），用于订阅变更后判断影响面。
	Revision string `json:"revision,omitempty"`
}

// deviceState 是一台设备的下发状态。
type deviceState struct {
	DeviceID string `json:"device_id"`
	// Revision 是最后一次成功落地的 revision。
	Revision string `json:"revision"`
	// Files 按 ID 索引。
	Files map[string]fileState `json:"files"`
	// UpdatedAt 是状态最后更新时间。
	UpdatedAt time.Time `json:"updated_at"`
	// Overrides 是最近若干条覆盖记录，供管理面展示（需求 6.2）。
	Overrides []proto.Override `json:"overrides,omitempty"`
}

// StateStore 管理设备下发状态。
//
// 每次写入都是整文件原子替换：状态文件不大，单人使用，
// 比自建增量日志更不容易出错。
type StateStore struct {
	dir string
	mu  sync.Mutex
}

// NewStateStore 构造状态存储。
func NewStateStore(dir string) (*StateStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建设备状态目录失败: %w", err)
	}
	return &StateStore{dir: dir}, nil
}

// defaultStateDir 返回仓库旁边的状态目录。
func defaultStateDir(storeDir string) string {
	return filepath.Join(filepath.Dir(storeDir), "state")
}

func (s *StateStore) path(deviceID string) string {
	return filepath.Join(s.dir, safeName(deviceID)+".json")
}

// safeName 把设备 ID 变成安全文件名。设备 ID 由服务端生成（dev-<hex>），
// 这里再滤一次，避免任何奇怪字符影响路径。
func safeName(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

// Load 读取一台设备的状态。不存在时返回空状态而不是错误。
func (s *StateStore) Load(deviceID string) (*deviceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(deviceID)
}

func (s *StateStore) loadLocked(deviceID string) (*deviceState, error) {
	data, err := os.ReadFile(s.path(deviceID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &deviceState{DeviceID: deviceID, Files: map[string]fileState{}}, nil
		}
		return nil, err
	}
	var st deviceState
	if err := json.Unmarshal(data, &st); err != nil {
		// 状态损坏不应该阻塞下发：丢弃旧状态，下次全量补齐即可（幂等）。
		return &deviceState{DeviceID: deviceID, Files: map[string]fileState{}}, nil
	}
	if st.Files == nil {
		st.Files = map[string]fileState{}
	}
	if st.DeviceID == "" {
		st.DeviceID = deviceID
	}
	return &st, nil
}

// saveLocked 原子写入状态。
func (s *StateStore) saveLocked(st *deviceState) error {
	st.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化设备状态失败: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(st.DeviceID))
}

// RecordAck 记录一次回执（需求 4.4 的幂等基础）。
//
// 约定：Hash 为空表示这条回执对应一次删除（删除没有内容可哈希）。
// 删除确认后从状态里移除该条目，否则它会在每次对账时重复产生删除下发。
func (s *StateStore) RecordAck(deviceID, revision string, results []proto.AckResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadLocked(deviceID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, res := range results {
		if !res.OK {
			// 失败的结果不更新状态：下次对账仍然会补发。
			continue
		}
		if res.Hash == "" {
			delete(st.Files, res.ID)
			continue
		}
		prev := st.Files[res.ID]
		prev.ID = res.ID
		prev.Hash = res.Hash
		prev.Paths = res.Paths
		prev.AppliedAt = now
		prev.Revision = revision
		st.Files[res.ID] = prev
	}
	st.Revision = revision
	return s.saveLocked(st)
}

// RecordPlan 在下发前把本次下发的形状记进状态。
//
// 目的是让“代理还没回执就断线”的情况下，下次对账仍知道这条曾是 Dec 下发的，
// 从而在条目被删除时能补发删除，而不是留下孤儿文件。
func (s *StateStore) RecordPlan(deviceID string, files []proto.File) error {
	if len(files) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadLocked(deviceID)
	if err != nil {
		return err
	}
	for _, f := range files {
		prev := st.Files[f.ID]
		prev.ID = f.ID
		prev.Format = f.Format
		prev.Key = f.Key
		prev.Secret = f.Secret
		st.Files[f.ID] = prev
	}
	return s.saveLocked(st)
}

// AddOverrides 记录服务端检测到的本地改动（需求 6.2）。
//
// 同一条目重复检测到同样的本地哈希时只保留一条：代理未回执前每次对账都会
// 重新检测到同一次改动，重复记录只会污染管理面。
func (s *StateStore) AddOverrides(deviceID string, overrides []proto.Override) error {
	if len(overrides) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadLocked(deviceID)
	if err != nil {
		return err
	}
	last := make(map[string]string, len(st.Overrides))
	for _, o := range st.Overrides {
		last[o.ID] = o.Hash
	}
	added := 0
	for _, o := range overrides {
		if last[o.ID] == o.Hash {
			continue
		}
		if o.DetectedAt.IsZero() {
			o.DetectedAt = time.Now().UTC()
		}
		st.Overrides = append(st.Overrides, o)
		last[o.ID] = o.Hash
		added++
	}
	if added == 0 {
		return nil
	}
	if len(st.Overrides) > maxOverrides {
		st.Overrides = st.Overrides[len(st.Overrides)-maxOverrides:]
	}
	return s.saveLocked(st)
}

// maxOverrides 是每台设备保留的覆盖记录条数。
const maxOverrides = 200

// Overrides 返回一台设备的覆盖记录，最近的在前面。
func (s *StateStore) Overrides(deviceID string) ([]proto.Override, error) {
	st, err := s.Load(deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]proto.Override, 0, len(st.Overrides))
	for i := len(st.Overrides) - 1; i >= 0; i-- {
		out = append(out, st.Overrides[i])
	}
	return out, nil
}

// Drift 计算设备与期望状态的差异条数，供管理面展示（需求 14.1）。
func (s *StateStore) Drift(deviceID string, want *Rendered) (int, error) {
	st, err := s.Load(deviceID)
	if err != nil {
		return 0, err
	}
	drift := 0
	for _, f := range want.Files {
		if st.Files[f.File.ID].Hash != f.Hash {
			drift++
		}
	}
	// 设备上还有、云端已不再期望的条目同样算漂移（需要删除）。
	for id := range st.Files {
		if _, ok := want.HashOf(id); !ok {
			drift++
		}
	}
	return drift, nil
}

// clear 删除一台设备的状态。设备吊销或删除时调用。
func (s *StateStore) clear(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(deviceID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Sync 处理一次对账，产出需要补发的 batch（需求 4.2、4.3、6.2）。
//
// 步骤：
//
//  1. 计算期望状态。渲染失败的条目已被 RenderDevice 跳过，
//     因此它们既不会出现在下发列表里，也不会被当作“不再期望”而删除，
//     落点自然保留上一次成功的内容（需求 4.5）。
//  2. 对每个期望条目比对代理上报的哈希：一致且形状一致就跳过，
//     保证重复下发不会触发改写（需求 4.4）。
//  3. 上报哈希与服务端记录的上次落地哈希不一致，说明落点被本地改动过。
//     这里直接下发期望内容覆盖，并返回覆盖事件（需求 6.2）。
//  4. 上回下发过、本次不再期望的条目下发删除，路径取自状态里记录的绝对路径，
//     因为代理不保存下发清单（需求 5.2）。
func (r *Renderer) Sync(dev ssot.Device, req proto.SyncRequest) (*proto.SyncResponse, error) {
	want, err := r.RenderDevice(dev)
	if err != nil {
		return nil, err
	}
	state, err := r.states.Load(dev.ID)
	if err != nil {
		return nil, err
	}

	local := make(map[string]proto.ManifestEntry, len(req.Manifest))
	for _, e := range req.Manifest {
		local[e.ID] = e
	}

	now := time.Now().UTC()
	var files []proto.File
	var overrides []proto.Override

	for _, f := range want.Files {
		cur, seen := local[f.File.ID]
		if seen && cur.Hash == f.Hash && cur.Format == f.File.Format && cur.Key == f.File.Key {
			continue
		}
		// 走到这里说明落点与期望不一致，本次会用期望内容覆盖。
		// 只要代理报上来的哈希和上次落地的哈希不同，就说明是本地改过，
		// 记一条覆盖事件（需求 6.2）。
		//
		// 空哈希不能当成「没改」跳过。代理用空哈希表达「我保证不了这个条目
		// 的状态」（ADR 0047）：多落点里只要有一份和别的不同就报空。以前这里
		// 用 cur.Hash != "" 做前置条件，结果多落点被改时覆盖照样发生，审计里
		// 却一条记录都没有——改了没人知道，比完全不检测更危险。
		if seen {
			if prev, ok := state.Files[f.File.ID]; ok && prev.Hash != cur.Hash {
				overrides = append(overrides, proto.Override{
					ID:         f.File.ID,
					Hash:       cur.Hash,
					Paths:      cur.Paths,
					DetectedAt: now,
				})
			}
		}
		files = append(files, f.File)
	}

	// 需要删除的条目：上回服务端下发过，本次不再期望。
	//
	// 渲染失败（有告警）的条目必须排除：它只是这次取不到密钥，
	// 落点仍应保留上一次成功的内容（需求 4.5），删掉就等于用空值渲染了。
	failed := make(map[string]bool, len(want.Alerts))
	for _, a := range want.Alerts {
		failed[a.ID] = true
	}
	gone := make([]string, 0, len(state.Files))
	for id := range state.Files {
		if failed[id] {
			continue
		}
		if _, ok := want.HashOf(id); ok {
			continue
		}
		gone = append(gone, id)
	}
	sort.Strings(gone)
	deletes := make([]proto.File, 0, len(gone))
	for _, id := range gone {
		st := state.Files[id]
		// 路径优先用代理这次上报的现场路径：代理不保存下发清单（需求 5.2），
		// 但每次对账都会上报所管理落点的当前路径，那是最可靠的定位依据。
		// 没有上报时退回上一次回执记下的路径。
		paths := st.Paths
		if cur, ok := local[id]; ok && len(cur.Paths) > 0 {
			paths = cur.Paths
		}
		targets := make([]proto.Target, 0, len(paths))
		for _, p := range paths {
			// 删除用绝对路径表达，代理直接删，不再做宿主展开。
			targets = append(targets, proto.Target{Plan: proto.PlanUser, Path: p})
		}
		deletes = append(deletes, proto.File{
			ID:      id,
			Targets: targets,
			Format:  st.Format,
			Key:     st.Key,
			Secret:  st.Secret,
		})
	}

	if len(files) == 0 && len(deletes) == 0 {
		return &proto.SyncResponse{Batch: nil, Accepted: true, Overrides: overrides, Checks: checksOf(want, state)}, nil
	}
	if err := r.states.RecordPlan(dev.ID, files); err != nil {
		return nil, err
	}
	if err := r.states.AddOverrides(dev.ID, overrides); err != nil {
		return nil, err
	}
	return &proto.SyncResponse{
		Batch: &proto.Batch{
			Revision: want.Revision,
			Files:    files,
			Deletes:  deletes,
		},
		Accepted:  true,
		Overrides: overrides,
		Checks:    checksOf(want, state),
	}, nil
}

// checksOf 生成“请核对这些落点”的清单（需求 5.2）。
//
// 取本次仍期望、且服务端已记下落点路径的条目：代理据此现场算哈希，
// 而不是自己维护一份下发清单。路径来自上一次回执，因此刚下发、还没回执的
// 条目不在清单里——它本来就需要被写一次，无需核对。
func checksOf(want *Rendered, state *deviceState) []proto.ManifestEntry {
	out := make([]proto.ManifestEntry, 0, len(want.Files))
	for _, f := range want.Files {
		st, ok := state.Files[f.File.ID]
		if !ok || len(st.Paths) == 0 {
			continue
		}
		out = append(out, proto.ManifestEntry{
			ID:     f.File.ID,
			Hash:   st.Hash,
			Paths:  st.Paths,
			Format: st.Format,
			Key:    st.Key,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Plan 返回一台设备当前需要补齐的内容与告警，不产生任何写入。
//
// 用于“资产刚提交，主动推给在线设备”这条路径：先算差异，再只推有变化的文件。
func (r *Renderer) Plan(dev ssot.Device, local []proto.ManifestEntry) (*Rendered, []proto.File, error) {
	want, err := r.RenderDevice(dev)
	if err != nil {
		return nil, nil, err
	}
	reported := make(map[string]string, len(local))
	for _, e := range local {
		reported[e.ID] = e.Hash
	}
	var out []proto.File
	for _, f := range want.Files {
		if h, ok := reported[f.File.ID]; ok && h == f.Hash {
			continue
		}
		out = append(out, f.File)
	}
	return want, out, nil
}

// Affected 返回订阅了某个 bundle 的全部设备。
//
// 写入资产后用它决定推给谁：用户平面订阅命中，或该项目订阅命中（需求 4.2）。
func (r *Renderer) Affected(bundle string) ([]ssot.Device, error) {
	devices, err := r.devices()
	if err != nil {
		return nil, err
	}
	projects, err := r.allProjects()
	if err != nil {
		return nil, err
	}
	// 项目 → 所属设备，用于把项目订阅折算到设备。
	projDevice := make(map[string]string, len(projects))
	projHit := map[string]bool{}
	for _, p := range projects {
		projDevice[p.ID] = p.DeviceID
		for _, b := range p.Bundles {
			if b == bundle {
				projHit[p.DeviceID] = true
				break
			}
		}
	}

	var out []ssot.Device
	for _, d := range devices {
		if d.Revoked {
			continue
		}
		hit := projHit[d.ID]
		if !hit {
			for _, b := range d.UserBundles {
				if b == bundle {
					hit = true
					break
				}
			}
		}
		if hit {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// devices 读取全部设备。
func (r *Renderer) devices() ([]ssot.Device, error) {
	table, err := ssot.ReadJSON[ssot.DeviceTable](r.store, ssot.FileDevices)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return table.Devices, nil
}

// allProjects 读取全部项目登记。
func (r *Renderer) allProjects() ([]ssot.Project, error) {
	table, err := ssot.ReadJSON[ssot.ProjectTable](r.store, ssot.FileProjects)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return table.Projects, nil
}

