package cloudsvc

import (
	"errors"
	"strconv"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 服务端全局设置（config/settings.json）。
//
// 目前只有一项：自动更新的定时检查间隔。它存在的理由是"这个值需要在不发版、
// 不重启服务的前提下调整"——发版当天想让大家更早对齐，平时又想少轮询，
// 靠命令行参数做不到。
//
// 权限取舍：
//   - 读对所有来源开放。代理每次握手都要拿到间隔，管理面也要显示它；
//     这两者都不涉及任何机密，拦住只会让代理退化成用默认值。
//   - 写只给管理面与 CLI。这个值决定全部设备的更新节奏，
//     让 MCP 来源能改它，等于把"一次提示注入改变所有设备的更新频率"变成可行。

// mSettings 读写全局设置。
//
// 读写同一个方法：字段是"给了就是改、不给就是读"，加设置项时不必再加方法。
func (s *Server) mSettings(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.SettingsRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "settings 载荷类型错误")
	}

	if in.UpdateCheckIntervalSec != nil {
		if !identity.CanManageDevices(req.Source) {
			return proto.Fail(req.ID, proto.ErrForbidden,
				"该来源不能修改全局设置；请在本机管理面修改")
		}
		saved, err := ssot.SetSettings(s.store, ssot.Settings{
			UpdateCheckIntervalSec: *in.UpdateCheckIntervalSec,
		}, authorOf(req.Source, dev))
		if err != nil {
			code := proto.ErrInternal
			if errors.Is(err, ssot.ErrBadSettings) {
				code = proto.ErrInvalid
			}
			return proto.Fail(req.ID, code, err.Error())
		}
		// 设置影响全部设备的行为，必须留痕：改了什么、谁改的、在哪个 revision。
		s.recordRevision(nil, audit.EventSettings, dev, req, "settings",
			"自动更新检查间隔改为 "+strconv.Itoa(saved.UpdateCheckIntervalSec)+" 秒",
			s.store.Head(), true)
		s.NotifyChanged()
	}

	settings, err := ssot.ReadSettings(s.store)
	if err != nil {
		return failFromError(req.ID, err)
	}
	out := &proto.SettingsResponse{
		UpdateCheckIntervalSec:    settings.UpdateCheckIntervalSec,
		MinUpdateCheckIntervalSec: ssot.MinUpdateCheckIntervalSec,
		MaxUpdateCheckIntervalSec: ssot.MaxUpdateCheckIntervalSec,
	}
	if !settings.UpdatedAt.IsZero() {
		out.UpdatedAt = settings.UpdatedAt.Format(time.RFC3339)
	}
	return okPayload(req.ID, out)
}
