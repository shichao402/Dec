package identity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// Registry 是设备与项目的登记表，存在 SSOT 仓库里。
//
// 所有变更都走 SSOT 事务，所以登记、吊销、改类型都会进 git 历史，
// 管理面能查到“谁在什么时候把哪台设备吊销了”。
type Registry struct {
	store *ssot.Store

	mu   sync.Mutex
	seen map[string]seenState
}

// NewRegistry 构造登记表。
func NewRegistry(store *ssot.Store) *Registry { return &Registry{store: store} }

// Devices 返回全部设备。
func (r *Registry) Devices() ([]ssot.Device, error) {
	table, err := ssot.ReadJSON[ssot.DeviceTable](r.store, ssot.FileDevices)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return table.Devices, nil
}

// Device 按 ID 查设备。
func (r *Registry) Device(id string) (ssot.Device, error) {
	devices, err := r.Devices()
	if err != nil {
		return ssot.Device{}, err
	}
	for _, d := range devices {
		if d.ID == id {
			return d, nil
		}
	}
	return ssot.Device{}, fmt.Errorf("%w: 设备 %s", ssot.ErrNotFound, id)
}

// Add 登记一台新设备。
func (r *Registry) Add(dev ssot.Device, author ssot.Author) error {
	if dev.ID == "" {
		return fmt.Errorf("设备 ID 不能为空")
	}
	if !dev.Kind.Valid() {
		return fmt.Errorf("设备类型 %q 不受支持", dev.Kind)
	}
	_, err := r.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := loadDevices(tx)
		if err != nil {
			return "", err
		}
		for _, d := range table.Devices {
			if d.ID == dev.ID {
				return "", fmt.Errorf("%w: 设备 %s 已登记", ssot.ErrConflict, dev.ID)
			}
		}
		if dev.RegisteredAt.IsZero() {
			dev.RegisteredAt = time.Now().UTC()
		}
		table.Devices = append(table.Devices, dev)
		sortDevices(table.Devices)
		return fmt.Sprintf("登记设备 %s（%s）", dev.Name, dev.ID), tx.WriteJSON(ssot.FileDevices, table)
	})
	return err
}

// Update 修改一台设备。
func (r *Registry) Update(id string, author ssot.Author, message string, fn func(d *ssot.Device)) (ssot.Device, error) {
	var updated ssot.Device
	_, err := r.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := loadDevices(tx)
		if err != nil {
			return "", err
		}
		idx := -1
		for i, d := range table.Devices {
			if d.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", fmt.Errorf("%w: 设备 %s", ssot.ErrNotFound, id)
		}
		dev := table.Devices[idx]
		fn(&dev)
		table.Devices[idx] = dev
		updated = dev
		if message == "" {
			message = fmt.Sprintf("更新设备 %s", id)
		}
		return message, tx.WriteJSON(ssot.FileDevices, table)
	})
	return updated, err
}

// Revoke 吊销一台设备。
//
// 吊销后立即生效：后续任何请求都会因为登记表里 Revoked=true 被拒绝（需求 18.7）。
// 证书本身不用改，因为服务端每次都查登记表，不依赖证书吊销列表。
func (r *Registry) Revoke(id, reason string, author ssot.Author) (ssot.Device, error) {
	now := time.Now().UTC()
	msg := fmt.Sprintf("吊销设备 %s", id)
	if reason != "" {
		msg += "：" + reason
	}
	return r.Update(id, author, msg, func(d *ssot.Device) {
		d.Revoked = true
		d.RevokedAt = &now
	})
}

// SetKind 切换设备类型。
func (r *Registry) SetKind(id string, kind ssot.DeviceKind, author ssot.Author) (ssot.Device, error) {
	if !kind.Valid() {
		return ssot.Device{}, fmt.Errorf("设备类型 %q 不受支持", kind)
	}
	return r.Update(id, author, fmt.Sprintf("设备 %s 类型改为 %s", id, kind), func(d *ssot.Device) {
		d.Kind = kind
	})
}

// Renew 续签证书信息。
func (r *Registry) Renew(id, serial string, notAfter time.Time, fingerprint string, author ssot.Author) (ssot.Device, error) {
	return r.Update(id, author, fmt.Sprintf("续签设备 %s 证书", id), func(d *ssot.Device) {
		d.CertSerial = serial
		d.CertNotAfter = notAfter
		if fingerprint != "" {
			d.Fingerprint = fingerprint
		}
	})
}

// Seen 记录一次成功交互。
//
// 这类更新很频繁，但都写进 git 会让历史噪声过大，
// 所以只更新内存里的时间戳，并在每次冷备推送前汇总成一次提交（由上层调用 FlushSeen 完成）。
func (r *Registry) Seen(id, version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]seenState{}
	}
	now := time.Now().UTC()
	r.seen[id] = seenState{at: now, version: version}
}

type seenState struct {
	at      time.Time
	version string
}

// FlushSeen 把累积的在线状态写入仓库。
func (r *Registry) FlushSeen(author ssot.Author) error {
	r.mu.Lock()
	pending := r.seen
	r.seen = nil
	r.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	_, err := r.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := loadDevices(tx)
		if err != nil {
			return "", err
		}
		changed := false
		for i := range table.Devices {
			st, ok := pending[table.Devices[i].ID]
			if !ok {
				continue
			}
			at := st.at
			table.Devices[i].LastSeen = &at
			if st.version != "" && st.version != table.Devices[i].ReportedVersion {
				table.Devices[i].ReportedVersion = st.version
			}
			changed = true
		}
		if !changed {
			return "", nil
		}
		return "更新设备在线状态", tx.WriteJSON(ssot.FileDevices, table)
	})
	return err
}

// Active 报告设备是否仍然可以访问云端。
func Active(dev ssot.Device, now time.Time) error {
	if dev.Revoked {
		return fmt.Errorf("%w: 设备 %s 已被吊销", ErrCertRevoked, dev.ID)
	}
	if !dev.CertNotAfter.IsZero() && now.After(dev.CertNotAfter.Add(ClockSkew)) {
		return fmt.Errorf("%w: 设备 %s 证书已过期", ErrCertExpired, dev.ID)
	}
	return nil
}

// CanReadSecret 报告这个来源能否读取密钥明文。
//
// 只有用户本人在本机管理面发起的请求可以看明文，MCP 来源永远不行（已定决策 12）。
func CanReadSecret(src proto.Source) bool {
	return src == proto.SourceAdmin || src == proto.SourceHostCLI
}

// CanManageDevices 报告这个来源能否管理设备。
//
// 同上：AI 通过 MCP 不能登记、吊销或改设备类型，避免一次提示注入就能把自己加进设备表。
func CanManageDevices(src proto.Source) bool {
	return src == proto.SourceAdmin || src == proto.SourceHostCLI
}

// CanWrite 报告这个来源能否写资产与密钥。
//
// MCP 可以写：这是日常主要用法，AI 帮用户整理资产。
// 但写入会进 git 历史，并能被一键回滚，所以风险可控。
func CanWrite(src proto.Source, dev ssot.Device) bool {
	if dev.Kind != ssot.DeviceMine {
		return false
	}
	switch src {
	case proto.SourceMCP, proto.SourceAdmin, proto.SourceHostCLI:
		return true
	}
	return false
}

// loadDevices 在事务内读取设备表。
func loadDevices(tx *ssot.Tx) (ssot.DeviceTable, error) {
	table, err := ssot.ReadJSON[ssot.DeviceTable](tx.Store(), ssot.FileDevices)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return ssot.DeviceTable{SchemaVersion: ssot.SchemaVersion}, nil
		}
		return ssot.DeviceTable{}, err
	}
	if table.SchemaVersion == 0 {
		table.SchemaVersion = ssot.SchemaVersion
	}
	return table, nil
}

func sortDevices(devices []ssot.Device) {
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].RegisteredAt.Equal(devices[j].RegisteredAt) {
			return devices[i].ID < devices[j].ID
		}
		return devices[i].RegisteredAt.Before(devices[j].RegisteredAt)
	})
}

// EnsureInitialized 报告是否已经有设备登记过。
func (r *Registry) EnsureInitialized() (bool, error) {
	devices, err := r.Devices()
	if err != nil {
		return false, err
	}
	return len(devices) > 0, nil
}

// Context 上携带设备身份的键，供 serve 层使用。
type ctxKey struct{}

// WithDevice 把设备信息放进 context。
func WithDevice(ctx context.Context, dev ssot.Device) context.Context {
	return context.WithValue(ctx, ctxKey{}, dev)
}

// DeviceFrom 从 context 取出设备信息。
func DeviceFrom(ctx context.Context) (ssot.Device, bool) {
	dev, ok := ctx.Value(ctxKey{}).(ssot.Device)
	return dev, ok
}
