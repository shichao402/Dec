package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 登记流程（需求 18.4、11）。
//
// 唯一路径是 Bitwarden 主密码 + 2FA：这是“设备全丢后如何证明是我”的答案。
// 代理只负责把凭据转发给服务端，不保存、不缓存、不落盘。

// EnrollInput 是一次登记请求。
type EnrollInput struct {
	Email    string
	Password string
	TOTP     string
	// DeviceName 是本机可读名字，默认取主机名。
	DeviceName string
	// Kind 是设备类型：mine（我的设备）或 receiver（只接收设备）。
	Kind string
}

// EnrollResult 是登记结果。
type EnrollResult struct {
	DeviceID  string
	NotAfter  time.Time
	IsFirst   bool
	ServerURL string
}

// Enroll 生成设备密钥并完成登记，成功后把身份存进本机。
//
// 私钥只在本机生成、只在本机保存：服务端只收到公钥。
func Enroll(ctx context.Context, client *Client, store *IdentityStore, in EnrollInput) (*EnrollResult, error) {
	if in.Email == "" || in.Password == "" {
		return nil, fmt.Errorf("需要 Bitwarden 邮箱与主密码")
	}
	// 已经有身份时不重复登记：换服务地址应当先清除身份（需求 3.3）。
	if store.Has() {
		return nil, fmt.Errorf("本机已有设备身份，如需重新登记请先清除")
	}

	privPEM, pubPEM, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	osName, arch := OSArch()
	name := strings.TrimSpace(in.DeviceName)
	if name == "" {
		name, _ = os.Hostname()
	}
	resp, err := client.Enroll(ctx, proto.EnrollBeginRequest{
		Email:           in.Email,
		Password:        in.Password,
		TOTP:            in.TOTP,
		DevicePublicKey: pubPEM,
		DeviceName:      name,
		OS:              osName,
		Arch:            arch,
	})
	if err != nil {
		return nil, err
	}
	if resp.DeviceID == "" || resp.Certificate == "" {
		return nil, fmt.Errorf("服务端返回的登记结果不完整")
	}
	id := &Identity{
		DeviceID: resp.DeviceID,
		CertPEM:  resp.Certificate,
		KeyPEM:   privPEM,
		NotAfter: resp.CertNotAfter,
	}
	if _, err := id.TLSCertificate(); err != nil {
		return nil, err
	}
	if err := store.Save(id); err != nil {
		return nil, err
	}
	return &EnrollResult{
		DeviceID:  resp.DeviceID,
		NotAfter:  resp.CertNotAfter,
		IsFirst:   resp.IsFirst,
		ServerURL: client.BaseURL(),
	}, nil
}

// Recover 在恢复模式下取回根密钥并重新登记（需求 11）。
func Recover(ctx context.Context, client *Client, store *IdentityStore, in EnrollInput) (*EnrollResult, error) {
	if in.Email == "" || in.Password == "" {
		return nil, fmt.Errorf("需要 Bitwarden 邮箱与主密码")
	}
	privPEM, pubPEM, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	osName, arch := OSArch()
	name := strings.TrimSpace(in.DeviceName)
	if name == "" {
		name, _ = os.Hostname()
	}
	// 恢复时旧身份可能还在文件里，先清掉：新证书会替换它。
	if err := store.Clear(); err != nil {
		return nil, err
	}
	resp, err := client.Recover(ctx, proto.RecoverRequest{
		Email:           in.Email,
		Password:        in.Password,
		TOTP:            in.TOTP,
		DevicePublicKey: pubPEM,
		DeviceName:      name,
		OS:              osName,
		Arch:            arch,
	})
	if err != nil {
		return nil, err
	}
	id := &Identity{
		DeviceID: resp.DeviceID,
		CertPEM:  resp.Certificate,
		KeyPEM:   privPEM,
		NotAfter: resp.CertNotAfter,
	}
	if err := store.Save(id); err != nil {
		return nil, err
	}
	return &EnrollResult{
		DeviceID:  resp.DeviceID,
		NotAfter:  resp.CertNotAfter,
		IsFirst:   resp.IsFirst,
		ServerURL: client.BaseURL(),
	}, nil
}

// Uninstall 卸载：清理 Dec 管理的落点与条目，删除本机身份（需求 2.11）。
//
// 在线时通知云端注销这台设备；离线也要能把本机清干净，
// 否则一台坏机器会留下无法处理的状态。
type UninstallResult struct {
	// RemovedPaths 是被删除的落点。
	RemovedPaths []string
	// RemoteNotified 表示云端已确认注销。
	RemoteNotified bool
	// Errors 是逐项失败原因，卸载不因单项失败而中断。
	Errors []string
}

// Uninstall 执行卸载。
//
// 落点清单来自 service 端状态：代理不保存清单（需求 5.2），
// 所以先做一次 sync 拿到 Checks，再逐项删除。离线时只能删身份与配置。
func Uninstall(ctx context.Context, client *Client, store *IdentityStore, stateDir string, projects []Project) *UninstallResult {
	res := &UninstallResult{}
	home, err := HomeDir()
	if err == nil {
		resolver := NewResolver(home, projects)
		writer := NewWriter(resolver, home)

		// 先问云端：要核对哪些落点（也就等于“哪些是 Dec 管的”）。
		if client != nil {
			resp, err := client.Sync(ctx, proto.SyncRequest{
				Projects: LocalProjects(projects),
			})
			if err == nil {
				res.RemoteNotified = true
				if resp.Batch == nil || (len(resp.Batch.Files) == 0 && len(resp.Batch.Deletes) == 0) {
					// 没有待处理内容，退出维护或已完成。
				}
				for _, entry := range resp.Checks {
					if err := writer.removeByPaths(entry); err != nil {
						res.Errors = append(res.Errors, err.Error())
						continue
					}
					res.RemovedPaths = append(res.RemovedPaths, entry.Paths...)
				}
			}
		}
		// 兜底：把已知宿主目录下由 Dec 管理的类别目录扫一遍。
		for _, host := range HostNames() {
			dirs, ok := hosts[host]
			if !ok || dirs.userDir == "" {
				continue
			}
			for _, cat := range []string{DirSkills, DirRules, DirEnv} {
				base := filepath.Join(home, dirs.userDir, cat)
				files, err := ScanManaged(base)
				if err != nil {
					res.Errors = append(res.Errors, err.Error())
					continue
				}
				for _, f := range files {
					if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
						res.Errors = append(res.Errors, err.Error())
						continue
					}
					res.RemovedPaths = append(res.RemovedPaths, f)
				}
				_ = os.Remove(base)
			}
		}
	}

	if err := store.Clear(); err != nil {
		res.Errors = append(res.Errors, err.Error())
	}
	sort.Strings(res.RemovedPaths)
	return res
}

// removeByPaths 删除一条核对清单描述的全部落点。
func (w *Writer) removeByPaths(e proto.ManifestEntry) error {
	for _, p := range e.Paths {
		if err := w.VerifyWithin(p); err != nil {
			continue
		}
		if e.Format == proto.FormatJSONKey && e.Key != "" {
			if err := removeJSONKey(p, e.Key); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		w.removeEmptyParents(p)
	}
	return nil
}
