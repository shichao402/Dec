// Package cloudsvc 实现 Dec Cloud 服务端主程序。
//
// 部署形态是“单一 HTTPS 端口 + 可选客户端证书”：
//
//   - TLS 在 443 上终止，客户端证书可选（不要求），因为登记与恢复流程
//     在拿到证书之前就要访问服务端（需求 16.3）。
//   - 拿到证书的后续请求，服务端从对端证书里取设备身份，
//     与 devices.json 交叉校验（吊销、过期、序列号）。
//   - 证书缺失且该方法要求身份时，直接拒绝；需要身份的方法清单在 proto 里固定。
//
// 无证书可访问的路径只有三个：登记/恢复（协议方法）、客户端下载、健康检查。
package cloudsvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shichao402/Dec/internal/cloud/alert"
	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/backup"
	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/localcert"
	"github.com/shichao402/Dec/internal/cloud/render"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// Config 是服务端运行配置。
type Config struct {
	// Domain 是服务域名，用于证书申请与下载索引页里的示例命令。
	Domain string
	// Listen 是监听地址，例如 "127.0.0.1:8444"（公网 443 由 nginx SNI 透传）。
	Listen string
	// DataDir 是数据根目录，下面有 ssot、state、certs 等子目录。
	DataDir string
	// ServerVersion 是服务端自身版本。
	ServerVersion string
	// RequiredVersion 是要求的代理版本。由发布清单声明（需求 19.1）。
	RequiredVersion string
	// AcceptAny 允许在没有 TLS 的情况下启动，仅用于本机调试与测试。
	AcceptAny bool

	// BackupURL 是冷备仓库地址（需求 15.1）。为空表示不推送。
	BackupURL string
	// BackupKey 是冷备仓库 deploy key 的路径。
	BackupKey string
	// AuditURL 是审计仓库地址（需求 15.3）。为空表示不推送。
	AuditURL string
	// AuditKey 是审计仓库 deploy key 的路径。
	AuditKey string
	// KnownHosts 是校验推送目标主机密钥的 known_hosts 文件路径。
	//
	// 为空时用 go-git 的默认查找（~/.ssh/known_hosts 与 /etc/ssh/ssh_known_hosts）。
	// 生产上显式指定：服务账号一般没有自己的 ~/.ssh，默认路径必然落空。
	KnownHosts string
	// WebhookSecret 是存放企业微信 webhook 的密钥名（需求 12.1）。
	WebhookSecret string

	// LocalTLSDomain 是指向 127.0.0.1 的本机子域，留空取 local.dec.firoyang.com。
	LocalTLSDomain string
	// LocalTLSZone 是该子域所在、托管在 DNSPod 上的主域名。
	LocalTLSZone string
	// LocalTLSSecret 是存放 DNSPod API Token 的密钥名；为空表示不签发本机证书。
	LocalTLSSecret string
	// ACMEDirectory 是签发本机证书用的 ACME 目录地址，留空取 Let's Encrypt 生产环境。
	ACMEDirectory string
	// ACMEEmail 是 ACME 账户联系邮箱，可空。
	ACMEEmail string

	// RootKeyPath 是根密钥文件路径。留空取 <DataDir>/root.key。
	//
	// 单独可配是为了需求 16.5：根密钥不能落在云厂商磁盘快照或镜像备份
	// 覆盖的位置。把数据目录整体快照、而根密钥另行放在快照之外，是最省事
	// 的满足方式，所以路径必须能从数据目录里拆出来。
	RootKeyPath string

	// VaultFactory 覆盖默认的 Bitwarden 访问实现，仅供测试替换。
	//
	// 生产必须留空：真实的冷备只能写进用户自己的 Bitwarden，
	// 允许外部注入等于给「悄悄把根密钥写到别处」留了后门。
	VaultFactory identity.VaultFactory
}

// Server 是云端服务。
type Server struct {
	cfg    Config
	store  *ssot.Store
	reg    *identity.Registry
	ca     *identity.CA
	boot   *identity.Bootstrapper
	assets *assets.Writer
	render *render.Renderer
	proj   *render.ProjectStore
	notify *render.Notifier

	// maintenance 表示服务正在部署，代理应进入离线降级（需求 21.4）。
	maintenance atomic.Bool
	// deployMu 串行化部署协作文件的消费：后台轮询与主动调用会同时发生。
	deployMu sync.Mutex
	// backupCheckedAt 是冷备最后一次校验成功的时间，由冷备模块写入（需求 10.5）。
	backupMu        sync.RWMutex
	backupCheckedAt map[string]time.Time

	// audit 是只追加审计日志（需求 15.3）。冷备未配置时仍然记本地。
	audit *audit.Log
	// alerts 是告警中心（需求 12）。
	alerts *alert.Hub
	// backup 是冷备与审计的推送调度（需求 15.1）。
	backup *backup.Service

	// ctx 是服务生命周期，用于后台协程。
	ctx    context.Context
	cancel context.CancelFunc

	httpSrv *http.Server
	// download 是客户端二进制下载目录，由部署上传。
	downloadDir string
	// limiter 限制下载频率（需求 20.4）。
	limiter *rateLimiter
	// secrets 用于取 webhook 地址（需求 12.1）。
	secrets *assets.Writer
	// localTLS 管理 local.dec.firoyang.com 证书（agent-tray-access 需求 4.4）。
	localTLS *localcert.Manager
}

// New 构造服务端，完成目录准备与各层装配。
func New(cfg Config) (*Server, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("缺少数据目录")
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = "dev"
	}
	// 要求版本由发布清单声明（需求 19.1）：部署脚本把它落在数据目录里，
	// 服务启动时读它，因此发布即生效，不需要另外手动配置。
	// 命令行参数只作调试兜底——文件不存在时才会用到。
	if v := readRequiredVersion(cfg.DataDir); v != "" {
		cfg.RequiredVersion = v
	}
	if cfg.RequiredVersion == "" {
		cfg.RequiredVersion = cfg.ServerVersion
	}
	// 需求 16.5：根密钥允许放在数据目录之外（例如不参与云盘快照的位置）。
	if cfg.RootKeyPath == "" {
		cfg.RootKeyPath = rootKeyPath(cfg.DataDir)
	}
	// 启动自检：根密钥目录必须可写。
	//
	// 这里的失败代价很高——用户得先输入主密码和二次验证码，走完整条初始化
	// 才发现目录不可写，然后重来。检查一下只是 open(2) 的代价，
	// 却能把「部署目录权限配错」这类问题在启动日志里直接点明。
	checkRootKeyWritable(cfg.RootKeyPath)

	dataDir := cfg.DataDir
	store, err := ssot.Open(storeDir(dataDir))
	if err != nil {
		return nil, err
	}
	ca, err := loadOrCreateCA(certDir(dataDir))
	if err != nil {
		return nil, err
	}
	reg := identity.NewRegistry(store)
	boot, err := identity.NewBootstrapper(identity.Options{
		Store:       store,
		CA:          ca,
		Registry:    reg,
		RootKeyPath: cfg.RootKeyPath,
		Vault:       cfg.VaultFactory,
	})
	if err != nil {
		return nil, err
	}
	if err := boot.LoadOrEnterRecovery(); err != nil {
		return nil, err
	}
	rend, err := render.New(render.Options{
		Store:    store,
		StateDir: stateDir(dataDir),
		RootKey:  boot.RootKey,
	})
	if err != nil {
		return nil, err
	}

	writer := assets.NewWriter(store)

	al, err := audit.Open(auditDir(dataDir))
	if err != nil {
		return nil, err
	}

	if err := prepareDeployDir(dataDir); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg:             cfg,
		store:           store,
		reg:             reg,
		ca:              ca,
		boot:            boot,
		assets:          writer,
		secrets:         writer,
		render:          rend,
		proj:            render.NewProjectStore(store),
		notify:          render.NewNotifier(),
		downloadDir:     downloadDir(dataDir),
		limiter:         newRateLimiter(30, time.Minute),
		audit:           al,
		backupCheckedAt: map[string]time.Time{},
		ctx:             ctx,
		cancel:          cancel,
	}
	// 带着维护标记启动时必须先进入维护状态，再开始对外服务（需求 21.4）。
	s.initMaintenanceState()
	s.alerts = alert.New(ctx, alert.Options{
		WebhookSecret: s.webhookURL,
		Logf:          s.logf,
	})
	s.backup = backup.NewService(backup.Options{
		BackupTarget: backup.Target{
			Name:       "冷备",
			Dir:        store.Dir(),
			URL:        cfg.BackupURL,
			KeyPath:    cfg.BackupKey,
			KnownHosts: cfg.KnownHosts,
		},
		AuditTarget: backup.Target{
			Name:       "审计",
			Dir:        al.Dir(),
			URL:        cfg.AuditURL,
			KeyPath:    cfg.AuditKey,
			KnownHosts: cfg.KnownHosts,
		},
		Reporter:   s,
		FlushAudit: al.Flush,
	})
	s.backup.Start(ctx)
	// 本机证书：后台周期检查并续期，未配置凭据时只记一次日志。
	s.localTLS = s.newLocalTLS()
	go s.localTLS.Run(ctx)
	// 部署协作文件由脚本跨进程写入，只能轮询跟进（需求 21.2、21.8）。
	s.syncDeployFiles()
	go s.watchDeploy(ctx)
	return s, nil
}

// 数据子目录约定。全部在 DataDir 之下，便于备份与迁移。
func storeDir(dataDir string) string    { return dataDir + "/ssot" }
func stateDir(dataDir string) string    { return dataDir + "/state" }
func certDir(dataDir string) string     { return dataDir + "/certs" }
func rootKeyPath(dataDir string) string { return dataDir + "/root.key" }
func downloadDir(dataDir string) string { return dataDir + "/download" }

// auditDir 是审计仓库目录。与 SSOT 仓库并列，但独立成库（需求 15.3）。
func auditDir(dataDir string) string { return dataDir + "/audit" }

// checkRootKeyWritable 在启动时检查根密钥目录是否可写，只告警不阻断。
//
// 不阻断是有意的：已经初始化过的服务只需要读根密钥，目录不可写完全能正常跑。
// 而没初始化过的服务如果目录不可写，用户会在输入完主密码和验证码之后才看到
// 一句 permission denied——所以这里提前说清楚，但不该让服务起不来。
func checkRootKeyWritable(rootKeyPath string) {
	dir := filepath.Dir(rootKeyPath)
	// 目录可能还不存在，Initialize 会自己建；这里只测最接近的真实目录。
	for d := dir; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			dir = d
			break
		}
	}
	f, err := os.CreateTemp(dir, ".writecheck-*")
	if err != nil {
		log.Printf("[cloud] 警告：根密钥目录 %s 不可写（%v）。"+
			"初始化会失败——写根密钥用「临时文件 + rename」的原子写，要求目录本身可写，"+
			"且该目录应归属服务账号。已初始化的服务不受影响。", dir, err)
		return
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
}

// requiredVersionFile 是发布清单里要求版本落成的文件。
//
// 用文件而不是命令行参数，是因为部署脚本要在服务停机期间把新值放好，
// 再让服务自己读。这样“发布即生效”（需求 19.1）不需要改 systemd 单元，
// 回滚时也能跟着快照一起回退。
func requiredVersionFile(dataDir string) string {
	return dataDir + "/required-version"
}

// readRequiredVersion 读取要求版本。文件缺失或内容为空时返回空串。
//
// 解析失败不报错：一个坏掉的文件不该拦住院服务启动，
// 退回到“与服务端版本对齐”是更安全的降级。
func readRequiredVersion(dataDir string) string {
	raw, err := os.ReadFile(requiredVersionFile(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// Store 返回 SSOT 仓库，供上层模块（冷备、审计）使用。
func (s *Server) Store() *ssot.Store { return s.store }

// Renderer 返回渲染器。
func (s *Server) Renderer() *render.Renderer { return s.render }

// Registry 返回设备登记表。
func (s *Server) Registry() *identity.Registry { return s.reg }

// Bootstrapper 返回初始化/登记/恢复流程。
func (s *Server) Bootstrapper() *identity.Bootstrapper { return s.boot }

// SetBackupCheckedAt 记录冷备最后一次校验成功的时间（需求 10.5）。
func (s *Server) SetBackupCheckedAt(t time.Time) { s.SetCheckedAt("backup", t) }

// SetCheckedAt 按通道记录校验时间。
func (s *Server) SetCheckedAt(kind string, t time.Time) {
	s.backupMu.Lock()
	if s.backupCheckedAt == nil {
		s.backupCheckedAt = map[string]time.Time{}
	}
	s.backupCheckedAt[kind] = t
	s.backupMu.Unlock()
}

// CheckedAt 返回某通道最后一次校验成功的时间。
func (s *Server) CheckedAt(kind string) *time.Time {
	s.backupMu.RLock()
	defer s.backupMu.RUnlock()
	if t, ok := s.backupCheckedAt[kind]; ok {
		out := t
		return &out
	}
	return nil
}

// Audit 返回审计日志。
func (s *Server) Audit() *audit.Log { return s.audit }

// Alerts 返回告警中心。
func (s *Server) Alerts() *alert.Hub { return s.alerts }

// Backup 返回冷备调度。
func (s *Server) Backup() *backup.Service { return s.backup }

// BackupFailed 实现 backup.Reporter。
func (s *Server) BackupFailed(kind string, err error) {
	s.logf("%s 推送失败: %v", kind, err)
	s.alerts.Raise(alert.KindBackupFailed, kind+"推送失败", err.Error())
}

// RemoteRewritten 实现 backup.Reporter。
func (s *Server) RemoteRewritten(kind string, err error) {
	s.logf("%s 远端历史被改写: %v", kind, err)
	s.alerts.Raise(alert.KindRemoteRewritten, kind+"远端历史被改写",
		err.Error()+"；已停止推送，请人工确认远端发生了什么")
}

// BackupChecked 实现 backup.Reporter。
func (s *Server) BackupChecked(kind string, at time.Time) { s.SetCheckedAt(kind, at) }

// webhookURL 从 Dec 密钥里取企业微信 webhook 地址（需求 12.1）。
//
// 每次发送时才取：服务启动时根密钥可能还没加载，用户也可能随时换地址。
// 取不到就返回空，告警走设备通道，不让 webhook 配置问题把告警整体憋死。
func (s *Server) webhookURL() (string, error) {
	name := s.cfg.WebhookSecret
	if name == "" {
		return "", nil
	}
	key := s.rootKeyOf()
	if key == nil {
		return "", nil
	}
	raw, err := s.secrets.GetSecret(key, name)
	if err != nil {
		// 没配过 webhook 是全新安装的正常状态，不是错误。
		// 这里返回 err 会让每条告警都往日志里刷一行失败，
		// 而告警本身其实已经通过设备通道送出去了。
		if errors.Is(err, ssot.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// NotifyChanged 广播内容变化，唤醒等待中的长轮询（需求 4.2）。
func (s *Server) NotifyChanged() { s.notify.Bump() }

// Handler 返回完整的 HTTP 处理器，便于测试直接注入 httptest。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// 健康检查：无证书可访问（需求 16.3、16.4）。
	mux.HandleFunc("/healthz", s.handleHealth)
	// 客户端下载：无证书可访问（需求 20）。
	mux.HandleFunc("/download/", s.handleDownload)
	mux.HandleFunc("/download", s.handleDownloadIndex)
	// 协议入口：单一端点，方法由 payload 里的 method 决定。
	mux.HandleFunc("/api", s.handleAPI)
	mux.HandleFunc("/api/", s.handleAPI)
	return mux
}

// Run 启动 HTTPS 服务并阻塞到 ctx 取消。
//
// TLS 用 TLS-ALPN-01 自动申请与续期（需求 7 的部署要求）。为了本机调试，
// AcceptAny 打开时使用自签临时证书，绝不用于生产。
func (s *Server) Run(ctx context.Context) error {
	handler := s.Handler()

	tlsCfg, err := s.tlsConfig()
	if err != nil {
		return err
	}
	s.httpSrv = &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", s.cfg.Listen, err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpSrv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// handleHealth 是无证书可访问的健康检查。
//
// 只暴露最少信息：可用性、版本、维护状态。不暴露设备数、revision 等内部状态，
// 因为这是公网端点。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              !s.maintenance.Load(),
		"server_version":  s.cfg.ServerVersion,
		"required_client": s.cfg.RequiredVersion,
		"protocol":        proto.Version,
		"maintenance":     s.maintenance.Load(),
	})
}

// 常见错误。
var (
	// ErrNoDevice 表示请求没有携带可识别的设备身份。
	ErrNoDevice = errors.New("no device identity")
	// ErrVersionOut 表示版本不对齐，只允许版本相关方法（需求 19.4）。
	ErrVersionOut = errors.New("version mismatch")
)

// deviceFrom 从 TLS 对端证书解析设备身份并与登记表交叉校验。
//
// 校验顺序：证书是否由本 CA 签发 → 本地设备记录 → 是否吊销 → 是否过期。
// 任何一步失败都返回 ErrNoDevice 的包装，让调用方统一拒绝。
func (s *Server) deviceFrom(r *http.Request) (ssot.Device, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ssot.Device{}, ErrNoDevice
	}
	// 只取终端证书；链里的其它证书不参与身份判定。
	leaf := r.TLS.PeerCertificates[0]
	leafPEM := pemEncodeCert(leaf.Raw)

	deviceID, serial, _, err := s.ca.VerifyDevice(leafPEM, time.Now())
	if err != nil {
		return ssot.Device{}, fmt.Errorf("%w: %v", ErrNoDevice, err)
	}
	dev, err := s.reg.Device(deviceID)
	if err != nil {
		return ssot.Device{}, fmt.Errorf("%w: 设备 %s 未登记", ErrNoDevice, deviceID)
	}
	if dev.Revoked {
		return ssot.Device{}, fmt.Errorf("%w: 设备 %s 已吊销", ErrNoDevice, deviceID)
	}
	// 序列号必须与登记的一致：吊销后重新登记会换新序列号，旧证书随之失效。
	if dev.CertSerial != "" && serial != "" && dev.CertSerial != serial {
		return ssot.Device{}, fmt.Errorf("%w: 设备 %s 的证书序列号不匹配", ErrNoDevice, deviceID)
	}
	if err := identity.Active(dev, time.Now()); err != nil {
		return ssot.Device{}, fmt.Errorf("%w: %v", ErrNoDevice, err)
	}
	return dev, nil
}

// aligned 报告代理版本是否达到服务端要求版本（需求 19.4）。
//
// 要求版本的语义是「最低可用版本」，不是「必须精确等于」：发布只改服务端时
// CI 会沿用上一个要求版本（需求 19.2），若按精确相等判定，已升级到新版本的
// 代理反而会判成不对齐而被拒绝服务。
func (s *Server) aligned(v string) bool {
	if v == "" {
		return true
	}
	return compareVersion(v, s.cfg.RequiredVersion) >= 0
}

// logf 是统一日志出口，便于后续接入审计与告警。
func (s *Server) logf(format string, args ...any) {
	log.Printf("[cloud] "+format, args...)
}

// trimMethod 把路径片段转成方法名，容忍前后斜杠。
func trimMethod(p string) string {
	return strings.Trim(strings.TrimSpace(p), "/")
}

// closeAll 用于测试收尾与进程退出。
func (s *Server) closeAll() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.backup != nil {
		s.backup.Stop()
	}
	if s.httpSrv != nil {
		_ = s.httpSrv.Close()
	}
}

// rootKeyOf 返回当前根密钥，供需要它的方法使用。
func (s *Server) rootKeyOf() *crypt.Key { return s.boot.RootKey() }
