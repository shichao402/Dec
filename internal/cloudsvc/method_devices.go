package cloudsvc

import (
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 设备管理（需求 14.1、18.2、18.7、18.11）。

// DevicesResponse 是设备列表。
type DevicesResponse struct {
	Devices []proto.DeviceInfo `json:"devices"`
}

// mDevices 返回设备列表与在线状态。
func (s *Server) mDevices(req *proto.Request, dev ssot.Device) *proto.Response {
	devices, err := s.reg.Devices()
	if err != nil {
		return failFromError(req.ID, err)
	}
	seen := s.notify.Devices()
	now := time.Now()

	out := make([]proto.DeviceInfo, 0, len(devices))
	for _, d := range devices {
		info := proto.DeviceInfo{
			ID:              d.ID,
			Name:            d.Name,
			Kind:            string(d.Kind),
			OS:              d.OS,
			Arch:            d.Arch,
			Revoked:         d.Revoked,
			RegisteredAt:    d.RegisteredAt,
			LastSeen:        d.LastSeen,
			ReportedVersion: d.ReportedVersion,
			CertNotAfter:    d.CertNotAfter,
			UserBundles:     d.UserBundles,
		}
		if at, ok := seen[d.ID]; ok {
			info.Online = now.Sub(at) < onlineWindow
			t := at
			info.LastSeen = &t
		}
		// 漂移条数：把渲染结果与状态比一次。渲染失败（例如根密钥未加载）时
		// 不算漂移，避免恢复模式下满屏红色。
		if !d.Revoked {
			if want, rerr := s.render.RenderDevice(d); rerr == nil {
				if drift, derr := s.render.States().Drift(d.ID, want); derr == nil {
					info.Drift = drift
				}
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return okPayload(req.ID, &DevicesResponse{Devices: out})
}

// onlineWindow 是判定“在线”的窗口。
//
// 代理有 60 秒长轮询，正常情况下每 60 秒内必有一次交互，
// 5 分钟窗口足以覆盖一次重连与退避。
const onlineWindow = 5 * time.Minute

// mRevokeDevice 吊销设备（需求 18.7）。
//
// 吊销后该设备既不能再连接，也不再接收下发；返回需要轮换的密钥清单（需求 18.11），
// 因为它曾经拿到过这些密钥的明文。
func (s *Server) mRevokeDevice(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.RevokeRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "revoke_device 载荷类型错误")
	}
	if err := requireDeviceAdmin(req.Source); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}
	if in.DeviceID == dev.ID {
		// 吊销自己会让服务端失去唯一的管理入口，是纯粹的误操作。
		return proto.Fail(req.ID, proto.ErrInvalid, "不能吊销当前设备自身")
	}
	if _, err := s.reg.Revoke(in.DeviceID, in.Reason, authorOf(req.Source, dev)); err != nil {
		return failFromError(req.ID, err)
	}
	secrets, err := s.assets.SecretsDeliveredTo(in.DeviceID)
	if err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	// 吊销是最关键的高危操作：必须留痕，并且立刻反映到冷备（需求 18.11）。
	s.recordRevision(nil, audit.EventRevoke, dev, req, in.DeviceID,
		"吊销设备，需轮换密钥 "+strings.Join(secrets, ","), s.store.Head(), true)
	return okPayload(req.ID, &proto.RevokeResponse{DeviceID: in.DeviceID, Secrets: secrets})
}

// mSetDeviceKind 切换设备类型（需求 18.2）。
func (s *Server) mSetDeviceKind(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.SetDeviceKindRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "set_device_kind 载荷类型错误")
	}
	if err := requireDeviceAdmin(req.Source); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}
	kind := ssot.DeviceKind(in.Kind)
	if !kind.Valid() {
		return proto.Fail(req.ID, proto.ErrInvalid, "设备类型必须是 mine 或 receiver")
	}
	updated, err := s.reg.SetKind(in.DeviceID, kind, authorOf(req.Source, dev))
	if err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventDeviceKind, dev, req, in.DeviceID,
		"设备类型改为 "+string(kind), s.store.Head(), true)
	return okPayload(req.ID, &proto.DeviceInfo{
		ID:           updated.ID,
		Name:         updated.Name,
		Kind:         string(updated.Kind),
		OS:           updated.OS,
		Arch:         updated.Arch,
		Revoked:      updated.Revoked,
		RegisteredAt: updated.RegisteredAt,
		UserBundles:  updated.UserBundles,
	})
}

// mRotationList 返回吊销后的密钥轮换清单（需求 18.11）。
//
// 判定“已轮换”的依据是密钥的 updated_at 是否晚于设备吊销时间：
// 换了新值就意味着旧设备拿到的那份已经作废。
func (s *Server) mRotationList(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	devices, err := s.reg.Devices()
	if err != nil {
		return failFromError(req.ID, err)
	}
	metas, err := s.assets.ListSecrets()
	if err != nil {
		return failFromError(req.ID, err)
	}

	// 每台已吊销设备对应一批待轮换密钥。
	var out []proto.RotationItem
	for _, d := range devices {
		if !d.Revoked || d.RevokedAt == nil {
			continue
		}
		for _, m := range metas {
			if !containsString(m.Devices, d.ID) {
				continue
			}
			rotated := m.UpdatedAt.After(*d.RevokedAt)
			item := proto.RotationItem{
				Secret:  m.Name,
				Devices: []string{d.ID},
				Rotated: rotated,
			}
			if rotated {
				ts := m.UpdatedAt.Format(time.RFC3339)
				item.RotatedAt = &ts
			}
			out = append(out, item)
		}
	}
	if out == nil {
		out = []proto.RotationItem{}
	}
	return okPayload(req.ID, &proto.RotationListResponse{Items: out})
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// 项目与订阅（需求 23.3）。

// mProjects 返回项目登记列表。
func (s *Server) mProjects(req *proto.Request, dev ssot.Device) *proto.Response {
	all, err := s.proj.List()
	if err != nil {
		return failFromError(req.ID, err)
	}
	devices, err := s.reg.Devices()
	if err != nil {
		return failFromError(req.ID, err)
	}
	nameOf := make(map[string]string, len(devices))
	for _, d := range devices {
		nameOf[d.ID] = d.Name
	}

	out := make([]proto.ProjectInfo, 0, len(all))
	for _, p := range all {
		out = append(out, proto.ProjectInfo{
			ID:           p.ID,
			DeviceID:     p.DeviceID,
			DeviceName:   nameOf[p.DeviceID],
			Name:         p.Name,
			Path:         p.Path,
			Bundles:      p.Bundles,
			RegisteredAt: p.RegisteredAt.Format(time.RFC3339),
		})
	}
	return okPayload(req.ID, &proto.ProjectsResponse{Projects: out})
}

// mPutProject 新增或修改项目登记。
func (s *Server) mPutProject(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.PutProjectRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "put_project 载荷类型错误")
	}
	saved, err := s.proj.Put(*in, authorOf(req.Source, dev))
	if err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventProject, dev, req, saved.ID,
		"登记项目 "+saved.Name+"（"+saved.Path+"）", s.store.Head(), true)
	return okPayload(req.ID, &proto.ProjectInfo{
		ID:           saved.ID,
		DeviceID:     saved.DeviceID,
		Name:         saved.Name,
		Path:         saved.Path,
		Bundles:      saved.Bundles,
		RegisteredAt: saved.RegisteredAt.Format(time.RFC3339),
	})
}

// mDeleteProject 删除项目登记。
func (s *Server) mDeleteProject(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.DeleteProjectRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "delete_project 载荷类型错误")
	}
	if _, err := s.proj.Delete(in.ID, authorOf(req.Source, dev)); err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventProject, dev, req, in.ID,
		"删除项目登记", s.store.Head(), true)
	return okPayload(req.ID, &proto.ProjectsResponse{Projects: nil})
}

// mSetSubscriptions 修改设备的用户平面订阅（需求 23.3）。
func (s *Server) mSetSubscriptions(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.SetSubscriptionsRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "set_subscriptions 载荷类型错误")
	}
	if err := requireDeviceSelfOrAdmin(req.Source, dev, in.DeviceID); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}
	updated, err := s.proj.SetSubscriptions(in.DeviceID, in.Bundles, authorOf(req.Source, dev))
	if err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventProject, dev, req, in.DeviceID,
		"更新用户平面订阅为 "+strings.Join(in.Bundles, ","), s.store.Head(), true)
	return okPayload(req.ID, &proto.DeviceInfo{
		ID:           updated.ID,
		Name:         updated.Name,
		Kind:         string(updated.Kind),
		OS:           updated.OS,
		Arch:         updated.Arch,
		Revoked:      updated.Revoked,
		RegisteredAt: updated.RegisteredAt,
		UserBundles:  updated.UserBundles,
	})
}

// mOverrides 返回某台设备的本地改动覆盖记录（需求 6.2、14.1）。
func (s *Server) mOverrides(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	// 没有参数时查询当前设备自己的记录；带 device_id 时查指定设备（仅管理面）。
	target := dev.ID
	if in, ok := payload.(*proto.OverridesRequest); ok && in.DeviceID != "" {
		if !identity.CanManageDevices(req.Source) && in.DeviceID != dev.ID {
			return proto.Fail(req.ID, proto.ErrForbidden, "该来源不能查看其它设备的覆盖记录")
		}
		target = in.DeviceID
	}
	list, err := s.render.States().Overrides(target)
	if err != nil {
		return failFromError(req.ID, err)
	}
	return okPayload(req.ID, &proto.OverridesResponse{DeviceID: target, Overrides: list})
}

// mRotate 轮换根密钥（需求 9.4）。
//
// 要求当场提供 Bitwarden 凭据：轮换会重写冷备，必须证明是账户主人。
func (s *Server) mRotate(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.RotateRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "rotate_root_key 载荷类型错误")
	}
	if !identity.CanManageDevices(req.Source) {
		return proto.Fail(req.ID, proto.ErrForbidden, "该来源不能轮换根密钥")
	}
	author := authorOf(req.Source, dev)
	resp, err := s.boot.Rotate(nil, *in, func(oldKey, newKey *crypt.Key) (int, error) {
		return s.assets.Rekey(oldKey, newKey, author)
	})
	if err != nil {
		return failFromError(req.ID, err)
	}
	if ts, perr := time.Parse(time.RFC3339, resp.BackupCheckedAt); perr == nil {
		s.SetBackupCheckedAt(ts)
	}
	s.NotifyChanged()
	// 轮换是最高危的操作，必须留痕（需求 9.4、15.3）。
	s.recordRevision(nil, audit.EventRootKeyRotate, dev, req,
		resp.NewKeyID, "根密钥从 "+resp.OldKeyID+" 轮换为 "+resp.NewKeyID,
		s.store.Head(), true)
	return okPayload(req.ID, resp)
}
