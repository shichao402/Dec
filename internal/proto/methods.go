package proto

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrUnknownMethod 表示收到未定义的方法。
	ErrUnknownMethod = errors.New("unknown method")
	// ErrBadPayload 表示 payload 无法解析成该方法对应的结构。
	ErrBadPayload = errors.New("bad payload")
)

// VersionMethods 是协议里永不破坏兼容的方法集合（需求 19.7）。
//
// 任何版本的代理都必须能调用它们，以保证旧代理总能收到更新提示、
// 也保证未登记的代理总能完成登记。
var VersionMethods = map[Method]bool{
	MethodHello:      true,
	MethodInitialize: true,
	MethodEnroll:     true,
	MethodRecover:    true,
}

// NoCertMethods 是无需设备证书即可访问的方法（需求 16.3 的例外清单）。
// 下载和健康检查走普通 HTTP 路径，不经过这套协议，因此不在这里。
var NoCertMethods = map[Method]bool{
	MethodInitialize: true,
	MethodEnroll:     true,
	MethodRecover:    true,
}

// OptionalCertMethods 是证书可选的协议方法（需求 19.7）。
//
// hello 必须能在没有证书时调用：旧代理升级、未登记的新机器、证书已过期，
// 这三种情况都要先问出“服务端要求的版本是多少”，才知道下一步做什么。
// 有证书时会顺带更新在线状态与上报版本，所以它是“可选”而不是“免”。
var OptionalCertMethods = map[Method]bool{
	MethodHello: true,
}

// methods 是所有受支持的方法及其 payload 构造函数。
// 新增方法必须登记在这里，serve 层据此做统一解析和错误映射。
var methods = map[Method]func() any{
	MethodHello:            func() any { return &HelloRequest{} },
	MethodSync:             func() any { return &SyncRequest{} },
	MethodAck:              func() any { return &AckRequest{} },
	MethodInitialize:       func() any { return &EnrollBeginRequest{} },
	MethodEnroll:           func() any { return &EnrollBeginRequest{} },
	MethodRecover:          func() any { return &RecoverRequest{} },
	MethodWriteAsset:       func() any { return &AssetPayload{} },
	MethodDeleteAsset:      func() any { return &AssetPayload{} },
	MethodPutSecret:        func() any { return &SecretPayload{} },
	MethodDeleteSecret:     func() any { return &SecretDeleteRequest{} },
	MethodReadSecret:       func() any { return &ReadSecretRequest{} },
	MethodListSecrets:      func() any { return &struct{}{} },
	MethodListAssets:       func() any { return &struct{}{} },
	MethodReadAsset:        func() any { return &ReadAssetRequest{} },
	MethodHistory:          func() any { return &HistoryRequest{} },
	MethodRollback:         func() any { return &RollbackRequest{} },
	MethodDevices:          func() any { return &struct{}{} },
	MethodRevokeDevice:     func() any { return &RevokeRequest{} },
	MethodSetDeviceKind:    func() any { return &SetDeviceKindRequest{} },
	MethodRotationList:     func() any { return &struct{}{} },
	MethodProjects:         func() any { return &struct{}{} },
	MethodPutProject:       func() any { return &PutProjectRequest{} },
	MethodDeleteProject:    func() any { return &DeleteProjectRequest{} },
	MethodSetSubscriptions: func() any { return &SetSubscriptionsRequest{} },
	MethodOverrides:        func() any { return &OverridesRequest{} },
	MethodRotateRootKey:    func() any { return &RotateRequest{} },
	MethodStatus:           func() any { return &struct{}{} },
}

// Known 报告方法是否受支持。
func Known(m Method) bool {
	_, ok := methods[m]
	return ok
}

// DecodePayload 把 Request.Payload 解析成方法对应的结构体指针。
func DecodePayload(req *Request) (any, error) {
	ctor, ok := methods[req.Method]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownMethod, req.Method)
	}
	target := ctor()
	if len(req.Payload) == 0 {
		return target, nil
	}
	if err := json.Unmarshal(req.Payload, target); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	return target, nil
}

// EncodePayload 把结构体序列化进 Request.Payload。
func (r *Request) EncodePayload(v any) error {
	if v == nil {
		r.Payload = nil
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.Payload = data
	return nil
}

// WithPayload 返回一个设置好 payload 的请求副本，便于调用方构造。
func WithPayload(m Method, src Source, v any) (*Request, error) {
	req := &Request{Method: m, Source: src}
	if err := req.EncodePayload(v); err != nil {
		return nil, err
	}
	return req, nil
}

// SecretDeleteRequest 删除密钥。
type SecretDeleteRequest struct {
	Name    string `json:"name"`
	Message string `json:"message,omitempty"`
	// Force 表示即使仍被引用也删除。默认拒绝并列出引用方（需求 8.4）。
	Force bool `json:"force,omitempty"`
}

// SecretDeleteResponse 是删除结果。
type SecretDeleteResponse struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

// HistoryRequest 查询历史。
type HistoryRequest struct {
	// Kind 是 asset 或 secret。
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}

// Revision 是一条历史记录。
type Revision struct {
	Commit    string `json:"commit"`
	Message   string `json:"message"`
	Author    string `json:"author"`
	Source    Source `json:"source"`
	CreatedAt string `json:"created_at"`
}

// HistoryResponse 是历史列表。
type HistoryResponse struct {
	Entries []Revision `json:"entries"`
}

// RollbackRequest 回滚到某个历史 revision。
//
// 回滚不是改写历史，而是把目标 revision 的内容以一个新 commit 写回去（需求 1.4），
// 所以恶意或误操作的回滚都能再被回滚。
type RollbackRequest struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Commit  string `json:"commit"`
	Message string `json:"message,omitempty"`
}

// RollbackResponse 是回滚结果。
type RollbackResponse struct {
	Revision string `json:"revision"`
	Commit   string `json:"commit"`
	Pushed   bool   `json:"pushed"`
}

// RevokeRequest 吊销设备。
type RevokeRequest struct {
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason,omitempty"`
}

// RevokeResponse 返回吊销后需要轮换的密钥清单。
type RevokeResponse struct {
	DeviceID string   `json:"device_id"`
	Secrets  []string `json:"secrets"`
}

// SetDeviceKindRequest 切换设备类型。
type SetDeviceKindRequest struct {
	DeviceID string `json:"device_id"`
	Kind     string `json:"kind"`
}

// RotationListResponse 是吊销后的轮换进度清单（需求 18.11）。
type RotationListResponse struct {
	Items []RotationItem `json:"items"`
}

// RotationItem 是一条待轮换的密钥及其进度。
type RotationItem struct {
	Secret  string   `json:"secret"`
	Devices []string `json:"devices"`
	Rotated bool     `json:"rotated"`
	// RotatedAt 是最近一次写入新值的时间。
	RotatedAt *string `json:"rotated_at,omitempty"`
}

// PutProjectRequest 新增或修改项目登记。
type PutProjectRequest struct {
	ID       string   `json:"id,omitempty"`
	DeviceID string   `json:"device_id"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Bundles  []string `json:"bundles"`
}

// ProjectInfo 是管理面看到的项目。
type ProjectInfo struct {
	ID           string   `json:"id"`
	DeviceID     string   `json:"device_id"`
	DeviceName   string   `json:"device_name"`
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Bundles      []string `json:"bundles"`
	RegisteredAt string   `json:"registered_at"`
}

// ProjectsResponse 是项目列表。
type ProjectsResponse struct {
	Projects []ProjectInfo `json:"projects"`
}

// DeleteProjectRequest 删除项目登记。
type DeleteProjectRequest struct {
	ID string `json:"id"`
}

// SetSubscriptionsRequest 修改设备的用户平面订阅。
type SetSubscriptionsRequest struct {
	DeviceID string   `json:"device_id"`
	Bundles  []string `json:"bundles"`
}

// RotateRequest 轮换根密钥。当前 root key 的轮换需要当场提供 Bitwarden 凭据（需求 9.4）。
type RotateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp,omitempty"`
}

// RotateResponse 返回新的 key id。
type RotateResponse struct {
	OldKeyID string `json:"old_key_id"`
	NewKeyID string `json:"new_key_id"`
	// BackupCheckedAt 是完成冷备读回校验的时间，管理面据此展示（需求 10.5）。
	BackupCheckedAt string `json:"backup_checked_at"`
}
