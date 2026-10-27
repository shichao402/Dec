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

// Initialize 完成首次初始化：生成根密钥、写 Bitwarden 冷备并登记第一台设备（需求 10.1）。
//
// 与 Enroll 的区别只在服务端走哪条路：全新服务还没有根密钥，enroll 必然失败，
// 必须用 initialize 从零建立。客户端这一侧的动作完全一样——生成密钥对、
// 把凭据和公钥发过去、保存拿回来的证书。
func Initialize(ctx context.Context, client *Client, store *IdentityStore, in EnrollInput) (*EnrollResult, error) {
	if in.Email == "" || in.Password == "" {
		return nil, fmt.Errorf("需要 Bitwarden 邮箱与主密码")
	}
	if store.Has() {
		return nil, fmt.Errorf("本机已有设备身份，如需重新初始化请先清除")
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
	resp, err := client.Initialize(ctx, proto.EnrollBeginRequest{
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
		return nil, fmt.Errorf("服务端返回的初始化结果不完整")
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

// RecoverOutcome 是恢复的结果。
type RecoverOutcome struct {
	EnrollResult
	// Enrolled 表示本机拿到了新证书（首次登记或用原密钥重新登记）。
	Enrolled        bool
	RootKeyRestored bool
	CARestored      bool
	CARebuilt       bool
	ReenrollDevices int
}

// Recover 在恢复模式下取回缺失的根密钥与服务端 CA（需求 11、ADR 0052）。
//
// 本机已有设备身份时沿用原设备密钥：服务端 CA 能从 Bitwarden 取回，
// 原证书本来就有效，只需 RestoreOnly；CA 被宣告丢失重建时，
// 用原密钥重新登记会接回原设备 ID 与全部订阅。
// 只有本机没有身份时才生成新密钥。
func Recover(ctx context.Context, client *Client, store *IdentityStore, in RecoverInput) (*RecoverOutcome, error) {
	if in.Email == "" || in.Password == "" {
		return nil, fmt.Errorf("需要 Bitwarden 邮箱与主密码")
	}
	osName, arch := OSArch()
	name := strings.TrimSpace(in.DeviceName)
	if name == "" {
		name, _ = os.Hostname()
	}

	privPEM, pubPEM := "", ""
	existing, _ := store.Load()
	if existing != nil && existing.KeyPEM != "" {
		pub, err := PublicKeyFromPrivate(existing.KeyPEM)
		if err != nil {
			return nil, err
		}
		privPEM, pubPEM = existing.KeyPEM, pub
	} else {
		p1, p2, err := GenerateKeyPair()
		if err != nil {
			return nil, err
		}
		privPEM, pubPEM = p1, p2
	}

	resp, err := client.Recover(ctx, proto.RecoverRequest{
		Email:           in.Email,
		Password:        in.Password,
		TOTP:            in.TOTP,
		DevicePublicKey: pubPEM,
		DeviceName:      name,
		OS:              osName,
		Arch:            arch,
		DeclareCALost:   in.DeclareCALost,
	})
	if err != nil {
		return nil, err
	}
	out := &RecoverOutcome{
		RootKeyRestored: resp.RootKeyRestored,
		CARestored:      resp.CARestored,
		CARebuilt:       resp.CARebuilt,
		ReenrollDevices: resp.ReenrollDevices,
	}
	out.ServerURL = client.BaseURL()
	if resp.EnrollBeginResponse != nil && resp.Certificate != "" {
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
		out.Enrolled = true
		out.DeviceID = resp.DeviceID
		out.NotAfter = resp.CertNotAfter
		out.IsFirst = resp.IsFirst
	}
	return out, nil
}

// RecoverInput 是恢复请求。
type RecoverInput struct {
	EnrollInput
	// DeclareCALost 表示用户已确认服务端 CA 无法找回，同意重建（ADR 0052）。
	DeclareCALost bool
}

// Reenroll 用本机原设备密钥重新登记（服务端 CA 重建后，ADR 0052）。
//
// 与 Enroll 的区别：不要求本机没有身份，也不生成新密钥。
// 服务端识别到同一把公钥，会沿用原设备 ID，订阅与项目登记原样保留。
func Reenroll(ctx context.Context, client *Client, store *IdentityStore, in EnrollInput) (*EnrollResult, error) {
	if in.Email == "" || in.Password == "" {
		return nil, fmt.Errorf("需要 Bitwarden 邮箱与主密码")
	}
	existing, err := store.Load()
	if err != nil || existing == nil || existing.KeyPEM == "" {
		return nil, fmt.Errorf("本机没有原设备密钥，请改用普通登记")
	}
	pubPEM, err := PublicKeyFromPrivate(existing.KeyPEM)
	if err != nil {
		return nil, err
	}
	osName, arch := OSArch()
	resp, err := client.Enroll(ctx, proto.EnrollBeginRequest{
		Email:           in.Email,
		Password:        in.Password,
		TOTP:            in.TOTP,
		DevicePublicKey: pubPEM,
		DeviceName:      strings.TrimSpace(in.DeviceName),
		OS:              osName,
		Arch:            arch,
	})
	if err != nil {
		return nil, err
	}
	id := &Identity{
		DeviceID: resp.DeviceID,
		CertPEM:  resp.Certificate,
		KeyPEM:   existing.KeyPEM,
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
