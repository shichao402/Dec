package cloudsvc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/render"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 协议分发：单一端点 /api，方法由 Request.Method 决定。
//
// 处理顺序是固定的，任何一步失败都不继续：
//
//  1. 解析外壳 → 2. 方法是否受支持 → 3. 维护状态 → 4. 版本对齐
//  → 5. 身份（按方法判定是否需要证书）→ 6. 权限 → 7. 业务处理。
//
// 顺序本身就是安全设计：版本不对齐的设备连身份都不必校验，
// 因为它只能拿到“你需要更新”这一个信息。

// maxBody 限制请求体大小。代理只上报清单与写入内容，10 MiB 足够。
const maxBody = 10 << 20

// maxWaitSeconds 是长轮询允许的最长挂起时间。
//
// 上限压在 60 秒：中间如果有反代或 NAT，太长的空闲连接容易被切断，
// 而 60 秒的代价只是空对账（幂等，无副作用）。
const maxWaitSeconds = 60

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, "读取请求失败", http.StatusBadRequest)
		return
	}
	if len(body) > maxBody {
		http.Error(w, "请求过大", http.StatusRequestEntityTooLarge)
		return
	}

	var req proto.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeProto(w, proto.Fail("", proto.ErrInvalid, "请求不是合法 JSON"))
		return
	}
	resp := s.dispatch(r, &req)
	writeProto(w, resp)
}

// writeProto 输出统一响应外壳。
func writeProto(w http.ResponseWriter, resp *proto.Response) {
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON 输出 JSON。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// dispatch 是协议处理的主干。
func (s *Server) dispatch(r *http.Request, req *proto.Request) *proto.Response {
	id := req.ID

	// 2. 方法必须受支持。未知方法不猜，直接拒绝。
	if !proto.Known(req.Method) {
		return proto.Fail(id, proto.ErrInvalid, "不支持的方法: "+string(req.Method))
	}

	// 3. 维护状态：除 hello 与登记/恢复外，一律告知维护中。
	// hello 必须放行，否则代理无法得知维护状态，只会不停重连。
	if s.maintenance.Load() && req.Method != proto.MethodHello {
		if !proto.NoCertMethods[req.Method] {
			return proto.Fail(id, proto.ErrMaintenance, "服务正在部署，请稍后重试")
		}
	}

	// 4. 版本对齐。未对齐时只放行协议里永不破坏兼容的那三个方法（需求 19.4、19.7）。
	if !s.aligned(req.RequestedVersion) && !proto.VersionMethods[req.Method] {
		resp := proto.Fail(id, proto.ErrVersionMismatch,
			"客户端版本 "+req.RequestedVersion+" 与服务端要求版本 "+s.cfg.RequiredVersion+" 不一致")
		resp.RequiredVersion = s.cfg.RequiredVersion
		return resp
	}

	// 4.5 恢复模式：根密钥不在，所有密文都打不开，业务接口必须一律拒绝（需求 9.4）。
	//
	// 只放行 hello 与恢复。hello 要放行是因为代理得能问出「服务在恢复模式」，
	// 否则它只会看到一连串 unauthorized 而不知道要去做恢复。
	// 这一步必须排在身份校验之前：恢复模式下服务端可能已经换了 CA
	// （服务器重建时 certs/ 一起没了），旧证书本来就验不过，
	// 先拦在这里才能给出准确原因，而不是把它误报成设备不存在。
	if s.boot.State().RecoveryMode {
		switch req.Method {
		case proto.MethodHello, proto.MethodRecover:
		default:
			return proto.Fail(id, proto.ErrRecoveryMode,
				"服务处于恢复模式（根密钥缺失），请先完成根密钥恢复")
		}
	}

	// 4.6 服务端 CA 不可用：设备证书没法校验，也没法签发（ADR 0052）。
	//
	// 与根密钥恢复模式一样排在身份校验之前：否则代理看到的只是
	// "设备未登记"之类的误导信息，不知道该去做 CA 恢复。
	if st := s.boot.CAStatus(); st.State != identity.CAReady {
		switch req.Method {
		case proto.MethodHello, proto.MethodRecover:
		default:
			return proto.Fail(id, proto.ErrCAPendingRestore,
				"服务端 CA 不可用（"+st.Detail+"），请先在本机页面或 dec-agent recover 完成恢复")
		}
	}

	// 5. 身份。免证书方法跳过；证书可选方法尽力取身份，取不到也放行。
	var dev ssot.Device
	if !proto.NoCertMethods[req.Method] {
		got, err := s.deviceFrom(r)
		switch {
		case err == nil:
			dev = got
		case proto.OptionalCertMethods[req.Method]:
			// hello 在无证书时也要能返回要求版本，否则旧代理没有自救通道（需求 19.7）。
			dev = ssot.Device{}
		default:
			// 恢复模式下的设备可能已经没有有效证书，但改设备类型这类操作本身不需要设备证书，
			// 它只在 SourceAdmin/HostCLI 下放行，见各方法内部的权限检查。
			if errors.Is(err, ErrNeedsReenroll) {
				return proto.Fail(id, proto.ErrDeviceNeedsReenroll,
					"服务端 CA 已重建，本机证书已失效：请用 dec-agent reenroll 或本机页面重新登记（沿用原设备与订阅）")
			}
			return proto.Fail(id, proto.ErrUnauthorized, err.Error())
		}
	}

	// 6. 业务处理。
	payload, err := proto.DecodePayload(req)
	if err != nil {
		return proto.Fail(id, proto.ErrInvalid, err.Error())
	}
	return s.run(r, req, payload, dev)
}

// run 执行具体方法。
func (s *Server) run(r *http.Request, req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	id := req.ID
	switch req.Method {
	case proto.MethodHello:
		return s.mHello(req, payload, dev)

	case proto.MethodSync:
		return s.mSync(r, req, payload, dev)
	case proto.MethodAck:
		return s.mAck(req, payload, dev)

	case proto.MethodInitialize:
		return s.mInitialize(r, req, payload)
	case proto.MethodEnroll:
		return s.mEnroll(r, req, payload)
	case proto.MethodRecover:
		return s.mRecover(r, req, payload)

	case proto.MethodWriteAsset:
		return s.mWriteAsset(req, payload, dev)
	case proto.MethodDeleteAsset:
		return s.mDeleteAsset(req, payload, dev)
	case proto.MethodDeleteBundle:
		return s.mDeleteBundle(req, payload, dev)
	case proto.MethodListAssets:
		return s.mListAssets(req, dev)
	case proto.MethodReadAsset:
		return s.mReadAsset(req, payload, dev)

	case proto.MethodPutSecret:
		return s.mPutSecret(req, payload, dev)
	case proto.MethodDeleteSecret:
		return s.mDeleteSecret(req, payload, dev)
	case proto.MethodReadSecret:
		return s.mReadSecret(req, payload, dev)
	case proto.MethodListSecrets:
		return s.mListSecrets(req, dev)

	case proto.MethodHistory:
		return s.mHistory(req, payload, dev)
	case proto.MethodRollback:
		return s.mRollback(req, payload, dev)

	case proto.MethodDevices:
		return s.mDevices(req, dev)
	case proto.MethodRevokeDevice:
		return s.mRevokeDevice(req, payload, dev)
	case proto.MethodSetDeviceKind:
		return s.mSetDeviceKind(req, payload, dev)
	case proto.MethodRotationList:
		return s.mRotationList(req, payload, dev)

	case proto.MethodProjects:
		return s.mProjects(req, dev)
	case proto.MethodPutProject:
		return s.mPutProject(req, payload, dev)
	case proto.MethodDeleteProject:
		return s.mDeleteProject(req, payload, dev)
	case proto.MethodSetSubscriptions:
		return s.mSetSubscriptions(req, payload, dev)

	case proto.MethodOverrides:
		return s.mOverrides(req, payload, dev)

	case proto.MethodRotateRootKey:
		return s.mRotate(r, req, payload, dev)

	case proto.MethodStatus:
		return s.mStatus(req, dev)
	}
	return proto.Fail(id, proto.ErrInvalid, "方法未实现: "+string(req.Method))
}

// failFromError 把内部错误映射成稳定错误码。
//
// 代理只按错误码决定提示与行为，不解析文本，所以映射必须稳定。
func failFromError(id string, err error) *proto.Response {
	if err == nil {
		return proto.Fail(id, proto.ErrInternal, "未知错误")
	}
	var code proto.ErrorCode
	switch {
	case errors.Is(err, ssot.ErrRevisionMismatch):
		code = proto.ErrConflict
	case errors.Is(err, ssot.ErrConflict):
		code = proto.ErrConflict
	case errors.Is(err, ssot.ErrNotFound):
		code = proto.ErrNotFound
	case errors.Is(err, assets.ErrInUse):
		code = proto.ErrInUse
	case errors.Is(err, assets.ErrMissingSecret):
		code = proto.ErrInvalid
	case errors.Is(err, assets.ErrPlaintextSecret):
		code = proto.ErrInvalid
	case errors.Is(err, assets.ErrSchema):
		code = proto.ErrInvalid
	case errors.Is(err, render.ErrBadProject):
		code = proto.ErrInvalid
	case errors.Is(err, render.ErrProjectExists):
		code = proto.ErrConflict
	case errors.Is(err, identity.ErrNeed2FA):
		code = proto.ErrUnauthorized
	case errors.Is(err, identity.ErrRateLimited):
		code = proto.ErrRateLimited
	case errors.Is(err, identity.ErrBootstrapClosed):
		code = proto.ErrBootstrapClosed
	case errors.Is(err, identity.ErrRecoveryRequired):
		code = proto.ErrNotInitialized
	case errors.Is(err, identity.ErrCANotInBackup):
		code = proto.ErrCANotInBackup
	case errors.Is(err, identity.ErrCAUnavailable):
		code = proto.ErrCAPendingRestore
	case errors.Is(err, identity.ErrCertRevoked), errors.Is(err, identity.ErrCertExpired),
		errors.Is(err, identity.ErrCertInvalid):
		code = proto.ErrUnauthorized
	default:
		code = proto.ErrInternal
	}
	return proto.Fail(id, code, err.Error())
}

// okPayload 构造带 payload 的成功响应。
func okPayload(id string, v any) *proto.Response {
	data, err := json.Marshal(v)
	if err != nil {
		return proto.Fail(id, proto.ErrInternal, "序列化响应失败: "+err.Error())
	}
	return proto.OK(id, data)
}

// authorOf 按来源构造成交作者。
func authorOf(src proto.Source, dev ssot.Device) ssot.Author {
	name := dev.ID
	if name == "" {
		name = "cloud"
	}
	return ssot.Author{Name: name, Email: name + "@dec.local", Source: string(src)}
}

// requireWrite 检查来源是否有写权限（需求 18.2、已定决策 12）。
func requireWrite(src proto.Source, dev ssot.Device) error {
	if !identity.CanWrite(src, dev) {
		return fmt.Errorf("来源 %s 不能写入", src)
	}
	return nil
}

// requireDeviceAdmin 检查来源是否能管理设备。
func requireDeviceAdmin(src proto.Source) error {
	if !identity.CanManageDevices(src) {
		return fmt.Errorf("来源 %s 不能管理设备", src)
	}
	return nil
}

// requireDeviceSelfOrAdmin 检查来源能否修改指定设备的订阅。
//
// 订阅列表只决定「这台设备自己消费哪些 bundle」，不涉及别的设备，也不涉及
// 任何密钥，所以允许来自设备自身的 MCP / admin / hostcli 修改自己的订阅：
// AI 帮用户把某个 bundle 加进本机订阅是日常用法，全拦住会让 dec_set_subscriptions
// 这个工具变成永远调不通的摆设。
//
// 但跨设备一律拒绝：一条提示注入不该能把另一台机器的订阅改掉（吊销、改类型
// 这类真正的设备管理仍然只走 requireDeviceAdmin）。
func requireDeviceSelfOrAdmin(src proto.Source, dev ssot.Device, target string) error {
	if identity.CanManageDevices(src) {
		return nil
	}
	if target != "" && target == dev.ID {
		return nil
	}
	return fmt.Errorf("来源 %s 只能修改本机设备（%s）的订阅，不能修改 %q 的订阅", src, dev.ID, target)
}

// waitBudget 把请求里的等待秒数折算成时长，并夹到上限。
func waitBudget(sec int) time.Duration {
	if sec <= 0 {
		return 0
	}
	if sec > maxWaitSeconds {
		sec = maxWaitSeconds
	}
	return time.Duration(sec) * time.Second
}

// trimTrailing 去掉路径尾部的斜杠，用于下载路径解析。
func trimTrailing(p string) string { return strings.TrimSuffix(p, "/") }
