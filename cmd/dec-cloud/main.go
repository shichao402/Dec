// Command dec-cloud 是 Dec Cloud 的服务端主程序。
//
// 它把整台机器变成一个可控的 SSOT：资产、密钥、设备身份都在这里，
// 各终端只运行超薄代理，把渲染结果落到本机。
//
// 部署要点（见部署文档）：dec-cloud 只监听回环高位端口（默认 127.0.0.1:8444），
// 公网 443 由 nginx 持有，nginx 用 stream + ssl_preread 按 SNI 把 dec 域名的
// 原始 TLS 流透传过来（不终止 TLS）。因此设备证书认证与 ACME TLS-ALPN-01
// 挑战都仍由 dec-cloud 自己完成。nginx 若改成终止 TLS，所有设备都会变成未认证。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/shichao402/Dec/internal/cloudsvc"
	"github.com/shichao402/Dec/internal/proto"
)

// 由 CI 通过 -ldflags 注入。
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	var (
		dataDir     = flag.String("data-dir", defaultDataDir(), "数据目录（仓库、状态、证书、根密钥都在这里）")
		listen      = flag.String("listen", "127.0.0.1:8444", "监听地址；生产上由 nginx 在 443 按 SNI 透传到这里")
		domain      = flag.String("domain", "dec.firoyang.com", "对外域名，用于 ACME 证书")
		required    = flag.String("required-version", "", "要求的代理版本；为空时取服务端版本")
		acceptAny   = flag.Bool("accept-any", false, "调试用：使用自签证书，绝不用于生产")
		backupURL   = flag.String("backup-url", "", "冷备仓库地址（需求 15.1），留空则不推送")
		backupKey   = flag.String("backup-key", "", "冷备仓库 deploy key 路径")
		auditURL    = flag.String("audit-url", "", "审计仓库地址（需求 15.3），留空则不推送")
		auditKey    = flag.String("audit-key", "", "审计仓库 deploy key 路径")
		knownHosts  = flag.String("known-hosts", "", "校验推送目标主机密钥的 known_hosts 文件；为空取 ssh 默认查找路径")
		webhook     = flag.String("webhook-secret", "wecom-alert-webhook", "存放企业微信 webhook 的密钥名")
		localDomain = flag.String("local-tls-domain", "local.dec.firoyang.com", "指向 127.0.0.1 的本机子域")
		localZone   = flag.String("local-tls-zone", "firoyang.com", "本机子域所在的 DNSPod 主域名")
		localSecret = flag.String("local-tls-secret", "dnspod-token", "存放 DNSPod API Token 的密钥名；为空则不签发本机证书")
		acmeDir     = flag.String("acme-directory", "", "本机证书 ACME 目录地址；为空取 Let's Encrypt 生产环境")
		acmeEmail   = flag.String("acme-email", "", "ACME 账户联系邮箱")
		rootKey     = flag.String("root-key", "", "根密钥文件路径；为空时取 <data-dir>/root.key，生产上建议放在不参与磁盘快照的位置")
		showVersion = flag.Bool("version", false, "打印版本后退出")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("dec-cloud %s (%s)\n", Version, BuildTime)
		return
	}

	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		fatal("解析数据目录失败: %v", err)
	}

	srv, err := cloudsvc.New(cloudsvc.Config{
		Domain:          *domain,
		Listen:          *listen,
		DataDir:         dir,
		ServerVersion:   Version,
		RequiredVersion: *required,
		AcceptAny:       *acceptAny,
		BackupURL:       *backupURL,
		BackupKey:       *backupKey,
		AuditURL:        *auditURL,
		AuditKey:        *auditKey,
		KnownHosts:      *knownHosts,
		WebhookSecret:   *webhook,
		LocalTLSDomain:  *localDomain,
		LocalTLSZone:    *localZone,
		LocalTLSSecret:  *localSecret,
		ACMEDirectory:   *acmeDir,
		ACMEEmail:       *acmeEmail,
		RootKeyPath:     *rootKey,
	})
	if err != nil {
		fatal("启动失败: %v", err)
	}

	if *acceptAny {
		fmt.Fprintln(os.Stderr, "警告：--accept-any 已开启，仅用于本机调试")
	}
	fmt.Printf("dec-cloud %s 启动中，协议版本 %d，数据目录 %s\n", Version, proto.Version, dir)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil && err != context.Canceled {
		fatal("运行中断: %v", err)
	}
	fmt.Println("dec-cloud 已退出")
}

// defaultDataDir 返回默认数据目录。
//
// 生产部署由 systemd 指定 /var/lib/dec-cloud；本机调试落到用户目录，
// 避免需要 root 才能跑起来。
func defaultDataDir() string {
	if os.Geteuid() == 0 {
		return "/var/lib/dec-cloud"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".dec-cloud"
	}
	return filepath.Join(home, ".dec-cloud")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
