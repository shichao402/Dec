package cloudsvc

import (
	"net/http"

	"github.com/shichao402/Dec/internal/cloud/alert"
	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 登记与恢复（需求 10、11、18.3 至 18.9）。

// mEnroll 登记新设备。
//
// 这是给非第一台设备发放身份的唯一入口：必须提供 Bitwarden 主密码与 2FA。
// 服务端只把它当作“账户主人证明”，不保存密码、不建立会话、不写日志。
func (s *Server) mEnroll(r *http.Request, req *proto.Request, payload any) *proto.Response {
	in, ok := payload.(*proto.EnrollBeginRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "enroll 载荷类型错误")
	}
	resp, err := s.boot.Enroll(r.Context(), *in)
	if err != nil {
		// 登记尝试无论成败都留痕并告警（需求 18.6）：
		// 失败的那些才是最有价值的信号——那可能不是用户本人在试。
		s.record(r, audit.EventEnroll, ssot.Device{}, req, "登记失败", err.Error(), false)
		s.raise(alert.KindEnroll, "设备登记失败", err.Error())
		return failFromError(req.ID, err)
	}
	s.record(r, audit.EventEnroll, ssot.Device{ID: resp.DeviceID}, req,
		"登记成功 "+resp.DeviceID, "已签发设备证书", true)
	s.raise(alert.KindEnroll, "新设备登记成功", resp.DeviceID)
	// 新设备一出现就该拿到它的订阅内容，但首次下发由它自己 hello + sync 完成。
	// 这里广播一次，只是让其它挂起的长轮询不必陪着一起等。
	s.NotifyChanged()
	s.triggerBackup()
	return okPayload(req.ID, resp)
}

// mRecover 在恢复模式下取回根密钥并登记设备（需求 11）。
func (s *Server) mRecover(r *http.Request, req *proto.Request, payload any) *proto.Response {
	in, ok := payload.(*proto.RecoverRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "recover 载荷类型错误")
	}
	resp, err := s.boot.Recover(r.Context(), *in)
	if err != nil {
		s.record(r, audit.EventRecover, ssot.Device{}, req, "恢复失败", err.Error(), false)
		s.raise(alert.KindRecovery, "根密钥恢复失败", err.Error())
		return failFromError(req.ID, err)
	}
	s.record(r, audit.EventRecover, ssot.Device{ID: resp.DeviceID}, req,
		"恢复成功 "+resp.DeviceID, "根密钥已取回", true)
	s.raise(alert.KindRecovery, "根密钥已恢复", resp.DeviceID)
	s.NotifyChanged()
	s.triggerBackup()
	return okPayload(req.ID, resp)
}
