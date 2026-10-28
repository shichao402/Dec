// Command dec-agent 是 Dec 的超薄代理。
//
// 安装后只有一件事要做：配置服务地址，然后登记。之后它在后台常驻，
// 只主动向外连服务端，把云端渲染好的内容落到白名单位置。
//
// 用法：
//
//	dec-agent run         以服务方式运行（前台进程，由系统服务托管）
//	dec-agent configure   配置服务地址
//	dec-agent initialize  首次初始化（生成根密钥、写冷备、登记第一台设备）
//	dec-agent enroll      在 CLI 里登记（无桌面环境）
//	dec-agent recover     在 CLI 里恢复（无桌面环境）
//	dec-agent reenroll    服务端 CA 重建后用原设备密钥重新登记
//	dec-agent status      打印状态
//	dec-agent projects    管理本机项目登记
//	dec-agent install     注册系统服务
//	dec-agent uninstall   卸载：清理落点并删除本机身份
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/shichao402/Dec/internal/acceptance"
	"github.com/shichao402/Dec/internal/agent"
	"github.com/shichao402/Dec/internal/agent/legacy"
	"github.com/shichao402/Dec/internal/proto"
)

// Version 与 BuildTime 由构建时注入（-X main.Version=...），与其它命令保持一致。
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "run":
		err = cmdRun(args)
	case "configure":
		err = cmdConfigure(args)
	case "initialize":
		err = cmdInitialize(args)
	case "enroll":
		err = cmdEnroll(args)
	case "recover":
		err = cmdRecover(args)
	case "reenroll":
		err = cmdReenroll(args)
	case "status":
		err = cmdStatus(args)
	case "projects":
		err = cmdProjects(args)
	case "mcp-config":
		err = cmdMCPConfig(args)
	case "acceptance":
		err = cmdAcceptance(args)
	case "install":
		err = cmdInstall(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "version":
		fmt.Printf("dec-agent %s (%s)\n", Version, BuildTime)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "失败: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Dec 代理

  dec-agent run          以后台常驻方式运行
  dec-agent configure    配置服务地址
  dec-agent initialize   首次初始化（生成根密钥并登记第一台设备）
  dec-agent enroll       在 CLI 里登记设备
  dec-agent recover      在 CLI 里恢复（服务端根密钥或 CA 缺失时）
  dec-agent reenroll     用本机原设备密钥重新登记（服务端 CA 重建后）
  dec-agent status       打印当前状态
  dec-agent projects     列出本机项目登记
  dec-agent mcp-config   打印本机 MCP 条目与各宿主的写入状态
  dec-agent acceptance   对运行中的代理跑一遍真实链路验收（会临时写入并清理）
  dec-agent install      注册为系统服务
  dec-agent uninstall    卸载并清理
  dec-agent version      打印版本

状态目录默认 ~/.dec-agent，可用 DEC_AGENT_STATE 覆盖。
服务地址必须为 https。
`)
}

// stateDir 返回状态目录。
func stateDir() (string, error) {
	return agent.DefaultStateDir()
}

// mustState 取状态目录，失败直接退出。
func mustState() string {
	dir, err := stateDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "取状态目录失败: %v\n", err)
		os.Exit(1)
	}
	return dir
}

// cmdRun 以前台进程运行同步主循环。
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	insecure := fs.Bool("insecure", false, "跳过 TLS 校验（仅本机调试）")
	port := fs.Int("port", agent.DefaultPort, "本机页面端口")
	name := fs.String("name", "", "上报给服务端的设备名")
	// 宿主配置是全局的，不随状态目录隔离。用自定义状态目录跑第二个实例
	// （调试、验证、并行测试）时，它会按自己的端口改写 ~/.claude.json 等
	// 文件，把正在用的那个实例的条目顶掉。这个开关让这类实例只读不写。
	noHostSync := fs.Bool("no-host-sync", false, "不写入 IDE 的 MCP 配置（隔离实例用）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := mustState()

	// 日志只写一个文件（需求 5.1）。服务方式运行时 stdout/stderr 被重定向到
	// 同一文件，不能再回显，否则每行写两遍；只有前台在终端里跑才回显。
	logOpts := agent.LogFileOptions{Path: agent.LogPath(dir)}
	if isTerminal(os.Stderr) {
		logOpts.Echo = os.Stderr
	}
	logSink = agent.OpenLogFile(logOpts)
	defer logSink.Close()

	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("还没有配置服务地址，先执行 dec-agent configure --url https://...")
	}
	deviceName := *name
	if deviceName == "" {
		deviceName = cfg.DeviceName
	}

	store := agent.NewIdentityStore(dir)
	syncer, err := agent.NewSyncer(agent.Options{
		ServerURL:       cfg.ServerURL,
		Version:         Version,
		StateDir:        dir,
		DeviceName:      deviceName,
		Notifier:        &agent.SystemNotifier{Logf: logf},
		Logf:            logf,
		Insecure:        *insecure,
		LongPoll:        30 * time.Second,
		Heartbeat:       10 * time.Minute,
		DisconnectAlert: 30 * time.Minute,
	}, store)
	if err != nil {
		return err
	}
	if projects, err := agent.LoadProjects(dir); err == nil {
		syncer.SetProjects(projects)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// MCP 服务（需求 1）：协议层在本地完成，每次调用转发给云端，来源固定为 mcp。
	mcpHandler := agent.NewMCPHandler(agent.MCPServer{
		ServerVersion: Version,
		Status:        syncer.Status,
		Call: func(ctx context.Context, m proto.Method, payload json.RawMessage) (json.RawMessage, error) {
			return callRemote(ctx, syncer.Client(), m, proto.SourceMCP, payload)
		},
	})

	legacyScan := func() (*legacy.Report, error) {
		checks, ok := syncer.Checks()
		if !ok {
			return nil, legacy.ErrExpectedUnknown
		}
		home, err := agent.HomeDir()
		if err != nil {
			return nil, err
		}
		ps, _ := agent.LoadProjects(dir)
		roots := make([]string, 0, len(ps))
		for _, p := range ps {
			roots = append(roots, p.Path)
		}
		var self []string
		if exe, err := os.Executable(); err == nil {
			self = append(self, exe)
		}
		return legacy.Scan(legacy.Input{
			Home:           home,
			StateDir:       dir,
			Projects:       roots,
			Expected:       legacy.NewExpected(checks),
			ExtraProtected: self,
		})
	}
	// 工具页的旧版残留清理（.agent/legacy-cleanup 需求 1、3、6、8）。
	legacySvc := &legacy.Service{StateDir: dir, ScanFn: legacyScan, Logf: logf}

	// 本机页面与管理面转发（需求 2.10、13.1）。
	loop := agent.NewLoopback(agent.LoopbackOptions{
		Port:    *port,
		Logf:    logf,
		LogPath: agent.LogPath(dir),
		Status:  syncer.Status,
		MCP:     mcpHandler,
		Call: func(r *http.Request, m, src string, payload json.RawMessage) (json.RawMessage, error) {
			return callRemote(ctx, syncer.Client(), proto.Method(m), proto.Source(src), payload)
		},
		Enroll: func(r *http.Request, in agent.LoopbackEnrollInput) (*agent.EnrollResult, error) {
			return agent.Enroll(ctx, syncer.Client(), store, agent.EnrollInput{
				Email: in.Email, Password: in.Password, TOTP: in.TOTP, DeviceName: in.Device,
			})
		},
		Recover: func(r *http.Request, in agent.LoopbackEnrollInput) (*agent.RecoverOutcome, error) {
			return agent.Recover(ctx, syncer.Client(), store, agent.RecoverInput{
				EnrollInput: agent.EnrollInput{
					Email: in.Email, Password: in.Password, TOTP: in.TOTP, DeviceName: in.Device,
				},
				DeclareCALost: in.DeclareCALost,
			})
		},
		Reenroll: func(r *http.Request, in agent.LoopbackEnrollInput) (*agent.EnrollResult, error) {
			return agent.Reenroll(ctx, syncer.Client(), store, agent.EnrollInput{
				Email: in.Email, Password: in.Password, TOTP: in.TOTP, DeviceName: in.Device,
			})
		},
		Projects: func() []agent.Project {
			ps, _ := agent.LoadProjects(dir)
			return ps
		},
		SaveProjects: func(ps []agent.Project) error {
			if err := agent.SaveProjects(dir, ps); err != nil {
				return err
			}
			syncer.SetProjects(ps)
			return nil
		},
		Configure: func(url string) error { return configure(dir, store, cfg, url) },
		Legacy: &agent.LegacyTools{
			Scan:          func() (any, error) { return legacySvc.Scan() },
			Execute:       func(fp string) (any, error) { return legacySvc.Execute(fp) },
			Archives:      func() (any, error) { return legacySvc.Archives() },
			Archive:       func(id string) (any, error) { return legacySvc.Archive(id) },
			Restore:       func(id string, idx []int) (any, error) { return legacySvc.Restore(id, idx) },
			DeleteArchive: legacySvc.DeleteArchive,
		},
	})
	addr, err := loop.Start()
	if err != nil {
		return err
	}
	defer loop.Close()
	if err := agent.WriteAddrFile(dir, addr); err != nil {
		logf("写入监听地址文件失败（托盘将按默认端口查找）: %v", err)
	}

	// 把本机 MCP 条目写进已安装的宿主（需求 2.1、2.4）。
	// 端口是这次实际监听出来的地址，不是用户传的 --port：端口被占用时
	// 系统会换一个，条目必须指向真正能连上的那个。
	//
	// 只有默认服役实例才写：宿主配置是全局的，隔离实例（自定义状态目录的
	// 调试副本）按自己的端口改写它，会把正在用的那个实例的条目顶掉。
	if *noHostSync || !agent.IsPrimaryStateDir(dir) {
		logf("已跳过写入宿主 MCP 配置（非默认实例或显式关闭）")
	} else {
		for _, res := range agent.SyncHosts(addr) {
			switch res.Status {
			case agent.HostFailed:
				logf("写入 %s 的 MCP 配置失败: %s", res.Name, res.Reason)
			case agent.HostWritten:
				logf("已写入 %s 的 MCP 配置: %s", res.Name, res.Path)
			}
		}
	}

	logf("代理已启动，版本 %s，服务地址 %s，本机页面 http://%s", Version, cfg.ServerURL, addr)

	// 旧版残留归档的保留期（.agent/legacy-cleanup 需求 6.1）：启动时清一次，之后每天一次。
	go legacy.RunRetention(ctx, dir, 24*time.Hour, logf)

	if err := syncer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logf("代理退出")
	return nil
}

// cmdConfigure 配置服务地址。
func cmdConfigure(args []string) error {
	fs := flag.NewFlagSet("configure", flag.ExitOnError)
	url := fs.String("url", "", "服务地址，必须 https")
	name := fs.String("name", "", "设备名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" {
		return fmt.Errorf("需要 --url")
	}
	dir := mustState()
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	store := agent.NewIdentityStore(dir)
	if err := configure(dir, store, cfg, *url); err != nil {
		return err
	}
	if *name != "" {
		cfg.DeviceName = *name
		if err := agent.SaveConfig(dir, cfg); err != nil {
			return err
		}
	}
	fmt.Println("已保存。如果修改的是服务地址，本机身份已清除，请重新登记。")
	return nil
}

// configure 设置服务地址，地址变化时清除身份（需求 3.3）。
func configure(dir string, store *agent.IdentityStore, cfg *agent.Config, url string) error {
	changed := cfg.ServerURL != "" && cfg.ServerURL != url
	cfg.ServerURL = url
	if err := agent.SaveConfig(dir, cfg); err != nil {
		return err
	}
	if changed {
		if err := store.Clear(); err != nil {
			return err
		}
	}
	return nil
}

// cmdInitialize 完成首次初始化（需求 10.1）。
//
// 这是全新服务上第一台设备的入口，也是唯一会生成根密钥的地方。
// 完成后服务端的初始化入口永久关闭，后续设备只能走 enroll。
func cmdInitialize(args []string) error {
	fs := flag.NewFlagSet("initialize", flag.ExitOnError)
	email := fs.String("email", "", "Bitwarden 邮箱")
	password := fs.String("password", "", "Bitwarden 主密码（建议用环境变量 DEC_BW_PASSWORD）")
	totp := fs.String("totp", "", "两步验证码")
	name := fs.String("name", "", "设备名")
	insecure := fs.Bool("insecure", false, "跳过 TLS 校验（仅本机调试）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pw := *password
	if pw == "" {
		pw = os.Getenv("DEC_BW_PASSWORD")
	}
	c, err := collectCreds(*email, pw, *totp)
	if err != nil {
		return err
	}
	dir := mustState()
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("先执行 dec-agent configure --url https://...")
	}
	store := agent.NewIdentityStore(dir)
	client, err := agent.NewClient(agent.ClientOptions{
		BaseURL:  cfg.ServerURL,
		Version:  Version,
		Identity: store.Load2,
		Insecure: *insecure,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := agent.Initialize(ctx, client, store, agent.EnrollInput{
		Email: c.Email, Password: c.Password, TOTP: c.TOTP, DeviceName: *name,
	})
	if err != nil {
		return err
	}
	fmt.Printf("初始化完成：设备 %s，证书到期 %s\n", res.DeviceID, res.NotAfter.Format(time.RFC3339))
	fmt.Println("根密钥已生成并写入 Bitwarden 冷备，初始化入口已永久关闭。")
	return nil
}

// cmdEnroll 在 CLI 里登记。
func cmdEnroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	email := fs.String("email", "", "Bitwarden 邮箱")
	password := fs.String("password", "", "Bitwarden 主密码（建议用环境变量 DEC_BW_PASSWORD）")
	totp := fs.String("totp", "", "两步验证码")
	name := fs.String("name", "", "设备名")
	kind := fs.String("kind", "mine", "设备类型：mine 或 receiver")
	insecure := fs.Bool("insecure", false, "跳过 TLS 校验（仅本机调试）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pw := *password
	if pw == "" {
		pw = os.Getenv("DEC_BW_PASSWORD")
	}
	dir := mustState()
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("先执行 dec-agent configure --url https://...")
	}
	store := agent.NewIdentityStore(dir)
	client, err := agent.NewClient(agent.ClientOptions{
		BaseURL:  cfg.ServerURL,
		Version:  Version,
		Identity: store.Load2,
		Insecure: *insecure,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := collectCreds(*email, pw, *totp)
	if err != nil {
		return err
	}
	res, err := agent.Enroll(ctx, client, store, agent.EnrollInput{
		Email: c.Email, Password: c.Password, TOTP: c.TOTP, DeviceName: *name, Kind: *kind,
	})
	if err != nil {
		return err
	}
	fmt.Printf("登记成功：设备 %s，证书到期 %s\n", res.DeviceID, res.NotAfter.Format(time.RFC3339))
	if res.IsFirst {
		fmt.Println("这是第一台设备，初始化入口已关闭。")
	}
	return nil
}

// cmdRecover 在 CLI 里恢复。
func cmdRecover(args []string) error {
	fs := flag.NewFlagSet("recover", flag.ExitOnError)
	email := fs.String("email", "", "Bitwarden 邮箱")
	password := fs.String("password", "", "Bitwarden 主密码（建议用环境变量 DEC_BW_PASSWORD）")
	totp := fs.String("totp", "", "两步验证码")
	name := fs.String("name", "", "设备名")
	declareLost := fs.Bool("declare-ca-lost", false, "确认服务端 CA 已无法从 Bitwarden 找回，同意重建（所有设备需重新登记）")
	insecure := fs.Bool("insecure", false, "跳过 TLS 校验（仅本机调试）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pw := *password
	if pw == "" {
		pw = os.Getenv("DEC_BW_PASSWORD")
	}
	dir := mustState()
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("先执行 dec-agent configure --url https://...")
	}
	store := agent.NewIdentityStore(dir)
	client, err := agent.NewClient(agent.ClientOptions{
		BaseURL:  cfg.ServerURL,
		Version:  Version,
		Identity: store.Load2,
		Insecure: *insecure,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := collectCreds(*email, pw, *totp)
	if err != nil {
		return err
	}
	res, err := agent.Recover(ctx, client, store, agent.RecoverInput{
		EnrollInput: agent.EnrollInput{
			Email: c.Email, Password: c.Password, TOTP: c.TOTP, DeviceName: *name,
		},
		DeclareCALost: *declareLost,
	})
	if err != nil {
		if agent.CodeOf(err) == proto.ErrCANotInBackup {
			return fmt.Errorf("%w\n\n若确认这份 CA 已无法找回，加 --declare-ca-lost 重试：将生成新 CA，所有已登记设备都需要重新登记", err)
		}
		return err
	}
	printRecoverOutcome(res)
	return nil
}

// cmdReenroll 用本机原设备密钥重新登记（服务端 CA 重建后，ADR 0052）。
func cmdReenroll(args []string) error {
	fs := flag.NewFlagSet("reenroll", flag.ExitOnError)
	email := fs.String("email", "", "Bitwarden 邮箱")
	password := fs.String("password", "", "Bitwarden 主密码（建议用环境变量 DEC_BW_PASSWORD）")
	totp := fs.String("totp", "", "两步验证码")
	insecure := fs.Bool("insecure", false, "跳过 TLS 校验（仅本机调试）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pw := *password
	if pw == "" {
		pw = os.Getenv("DEC_BW_PASSWORD")
	}
	dir := mustState()
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("先执行 dec-agent configure --url https://...")
	}
	store := agent.NewIdentityStore(dir)
	client, err := agent.NewClient(agent.ClientOptions{
		BaseURL:  cfg.ServerURL,
		Version:  Version,
		Identity: store.Load2,
		Insecure: *insecure,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := collectCreds(*email, pw, *totp)
	if err != nil {
		return err
	}
	res, err := agent.Reenroll(ctx, client, store, agent.EnrollInput{
		Email: c.Email, Password: c.Password, TOTP: c.TOTP,
	})
	if err != nil {
		return err
	}
	fmt.Printf("重新登记成功：设备 %s（沿用原设备，订阅保留），证书到期 %s\n", res.DeviceID, res.NotAfter.Format(time.RFC3339))
	return nil
}

// printRecoverOutcome 打印恢复取回了什么，以及接下来要不要重新登记。
func printRecoverOutcome(res *agent.RecoverOutcome) {
	if res.RootKeyRestored {
		fmt.Println("根密钥已从 Bitwarden 取回。")
	}
	if res.CARestored {
		fmt.Println("服务端 CA 已从 Bitwarden 取回，已登记设备无需重新登记。")
	}
	if res.CARebuilt {
		fmt.Printf("服务端 CA 已重建，%d 台设备需要重新登记（在各设备上执行 dec-agent reenroll）。\n", res.ReenrollDevices)
	}
	if res.Enrolled {
		fmt.Printf("本机设备 %s 证书已更新，到期 %s\n", res.DeviceID, res.NotAfter.Format(time.RFC3339))
	}
}

// cmdStatus 打印状态。
func cmdStatus(args []string) error {
	dir := mustState()
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	store := agent.NewIdentityStore(dir)
	id := store.Load2()
	fmt.Printf("状态目录: %s\n", dir)
	fmt.Printf("服务地址: %s\n", fallback(cfg.ServerURL, "（未配置）"))
	fmt.Printf("代理版本: %s\n", Version)
	if id == nil {
		fmt.Println("设备身份: 未登记")
		return nil
	}
	fmt.Printf("设备身份: %s（证书到期 %s）\n", id.DeviceID, id.NotAfter.Format(time.RFC3339))
	projects, err := agent.LoadProjects(dir)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("项目登记: 无")
	} else {
		fmt.Println("项目登记:")
		sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
		for _, p := range projects {
			fmt.Printf("  %s  %s\n", p.ID, p.Path)
		}
	}
	return nil
}

// cmdProjects 管理项目登记。
func cmdProjects(args []string) error {
	if len(args) == 0 {
		dir := mustState()
		ps, err := agent.LoadProjects(dir)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Println("无项目登记")
			return nil
		}
		for _, p := range ps {
			fmt.Printf("%s\t%s\n", p.ID, p.Path)
		}
		return nil
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("projects add", flag.ExitOnError)
		id := fs.String("id", "", "项目 ID")
		path := fs.String("path", "", "项目绝对路径")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *path == "" {
			return fmt.Errorf("需要 --id 与 --path")
		}
		dir := mustState()
		ps, err := agent.LoadProjects(dir)
		if err != nil {
			return err
		}
		ps = append(ps, agent.Project{ID: *id, Path: *path})
		return agent.SaveProjects(dir, ps)
	case "remove":
		fs := flag.NewFlagSet("projects remove", flag.ExitOnError)
		id := fs.String("id", "", "项目 ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		dir := mustState()
		ps, err := agent.LoadProjects(dir)
		if err != nil {
			return err
		}
		out := ps[:0]
		for _, p := range ps {
			if p.ID != *id {
				out = append(out, p)
			}
		}
		return agent.SaveProjects(dir, out)
	}
	return fmt.Errorf("未知子命令: %s", args[0])
}

// cmdAcceptance 跑一遍真实链路验收（需求 4.1）。
//
// 它是 scripts/acceptance.sh 的实体：脚本负责找到二进制与参数，
// 这里负责按顺序检查并逐项打印结论。退出码非 0 表示有检查未通过。
func cmdAcceptance(args []string) error {
	fs := flag.NewFlagSet("acceptance", flag.ExitOnError)
	base := fs.String("url", "http://127.0.0.1:43679", "本机代理地址")
	bundle := fs.String("bundle", "", "临时 bundle 名，默认按时间戳生成")
	projectPath := fs.String("project", "", "临时项目目录，默认建在用户目录下的临时路径")
	timeout := fs.Duration("timeout", 90*time.Second, "单步等待下发落地的上限")
	keep := fs.Bool("keep", false, "即使全部通过也保留测试数据，便于人工观察")
	skipHosts := fs.Bool("skip-hosts", false, "跳过「各宿主 dec_vault 条目」检查（服务器上没有 IDE 时用）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *bundle == "" {
		*bundle = "dec-acceptance-" + time.Now().UTC().Format("20060102-150405")
	}
	if *projectPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*projectPath = filepath.Join(home, ".dec-acceptance", *bundle)
	}

	fmt.Printf("Dec 真实链路验收\n  代理：%s\n  临时 bundle：%s\n  临时项目：%s\n",
		*base, *bundle, *projectPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, err := acceptance.Run(ctx, acceptance.Options{
		BaseURL:     *base,
		Bundle:      *bundle,
		ProjectPath: *projectPath,
		Timeout:     *timeout,
		Keep:        *keep,
		SkipHosts:   *skipHosts,
	})
	if err != nil {
		return err
	}

	fmt.Println()
	if len(res.Residue) > 0 {
		fmt.Println("残留项（需要人工处理）：")
		for _, item := range res.Residue {
			fmt.Printf("  - %s\n", item)
		}
		fmt.Println()
	}
	if n := res.Failed(); n > 0 {
		return fmt.Errorf("验收未通过：%d 项检查失败", n)
	}
	if len(res.Residue) > 0 {
		return errors.New("检查全部通过，但仍有残留项")
	}
	fmt.Println("验收通过：所有检查项均通过，且没有留下测试资产。")
	return nil
}

// cmdMCPConfig 打印本机 MCP 条目与各宿主的写入状态（需求 2.6）。
//
// 排查「Agent 里看不到 Dec」时最常问的三个问题都能在这里找到答案：
// 条目长什么样、写到了哪个文件、为什么某个宿主没有写。
func cmdMCPConfig(args []string) error {
	fs := flag.NewFlagSet("mcp-config", flag.ExitOnError)
	port := fs.Int("port", agent.DefaultPort, "本机页面端口，用于拼出条目里的地址")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println("本机 MCP 入口:")
	fmt.Printf("  %s\n", agent.MCPServerURL(fmt.Sprintf("127.0.0.1:%d", *port)))
	fmt.Println()
	fmt.Println("宿主状态:")

	results := agent.InspectHosts()
	written := 0
	for _, res := range results {
		fmt.Printf("  %-8s %s  %s\n", res.Name, res.Status, res.Path)
		switch res.Status {
		case agent.HostWritten:
			written++
			for _, line := range strings.Split(strings.TrimSpace(res.Entry), "\n") {
				fmt.Printf("           %s\n", strings.TrimSpace(line))
			}
		default:
			if res.Reason != "" {
				fmt.Printf("           原因：%s\n", res.Reason)
			}
		}
	}
	fmt.Println()
	if written == 0 {
		fmt.Println("没有任何宿主已写入 `dec_vault` 条目。代理启动时会自动写入；")
		fmt.Println("如果宿主配置目录不存在，说明对应的 IDE 还没装，这是预期行为。")
	}
	return nil
}

// cmdInstall 注册系统服务。
func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	bin := fs.String("binary", "", "代理可执行文件路径，默认取当前进程")
	trayBin := fs.String("tray", "", "dec-tray 路径，默认在代理可执行文件旁边查找")
	noTray := fs.Bool("no-tray", false, "不安装托盘图标")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := *bin
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		path = exe
	}
	dir := mustState()
	if err := agent.InstallService(agent.ServiceOptions{
		BinaryPath: path,
		StateDir:   dir,
		LogDir:     filepath.Join(dir, "logs"),
	}); err != nil {
		return err
	}
	fmt.Println("系统服务已注册并启动。")
	installTray(path, *trayBin, *noTray, dir)
	return nil
}

// installTray 装托盘登录项。任何失败都只告警：代理已经装好，
// 托盘只是可见性组件，不能让它拖垮安装（需求 7.4）。
func installTray(agentBin, trayBin string, skip bool, dir string) {
	switch {
	case skip:
		return
	case !agent.TraySupported():
		fmt.Println("当前平台没有托盘组件，已跳过。")
		return
	}
	if trayBin == "" {
		p, err := agent.FindTrayBinary(agentBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告：%v，托盘图标未安装（代理不受影响）。\n", err)
			return
		}
		trayBin = p
	}
	if err := agent.InstallTray(agent.TrayOptions{
		BinaryPath: trayBin,
		StateDir:   dir,
		LogDir:     filepath.Join(dir, "logs"),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "警告：安装托盘图标失败: %v（代理不受影响）\n", err)
		return
	}
	fmt.Printf("托盘图标已安装并设为登录自启动：%s\n", trayBin)
}

// cmdUninstall 卸载。
func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	keepService := fs.Bool("keep-service", false, "保留系统服务定义")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := mustState()
	if !*keepService {
		if err := agent.UninstallTray(); err != nil {
			logf("移除托盘登录项失败: %v", err)
		}
		if err := agent.UninstallService(); err != nil {
			logf("移除系统服务失败: %v", err)
		}
	}
	cfg, err := agent.LoadConfig(dir)
	if err != nil {
		return err
	}
	store := agent.NewIdentityStore(dir)
	projects, _ := agent.LoadProjects(dir)

	var client *agent.Client
	if cfg.ServerURL != "" {
		client, _ = agent.NewClient(agent.ClientOptions{
			BaseURL:  cfg.ServerURL,
			Version:  Version,
			Identity: store.Load2,
			Timeout:  15 * time.Second,
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := agent.Uninstall(ctx, client, store, dir, projects)
	fmt.Printf("已删除 %d 个落点\n", len(res.RemovedPaths))
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  未能清理: %s\n", e)
	}
	if res.RemoteNotified {
		fmt.Println("已通知云端注销这台设备。")
	}

	// 从各宿主里移除 `dec_vault` 条目（需求 2.5）：卸载后 IDE 里留一条连不上的
	// MCP 服务器，用户每次打开 Agent 都会看到一个连接失败的错误。
	for _, hr := range agent.RemoveHosts() {
		switch hr.Status {
		case agent.HostRemoved:
			fmt.Printf("已从 %s 移除 dec_vault 条目\n", hr.Name)
		case agent.HostFailed:
			fmt.Fprintf(os.Stderr, "  从 %s 移除失败: %s\n", hr.Name, hr.Reason)
		}
	}
	return nil
}

// callRemote 把回环入口收到的一次调用转发给云端。
//
// payload 是调用方原样给出的 JSON，这里不解析成具体结构体：
// 代理不是业务逻辑的所在，解析即意味着代理要知道每个方法的细节，
// 一旦协议增加方法就得跟着发版。校验与含义全部由服务端负责。
func callRemote(ctx context.Context, client *agent.Client, m proto.Method, src proto.Source, payload json.RawMessage) (json.RawMessage, error) {
	if client == nil {
		return nil, agent.ErrNotConfigured
	}
	if !proto.Known(m) {
		return nil, fmt.Errorf("未知方法: %s", m)
	}
	var out json.RawMessage
	if err := client.Call(ctx, m, src, rawPayload(payload), &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return json.RawMessage("{}"), nil
	}
	return out, nil
}

// rawPayload 让空 payload 保持为 nil，而不是字面量 "null"。
//
// Request.EncodePayload 把 nil 编码成“无 payload”，而 json.RawMessage("null")
// 会被原样当成一段合法 JSON 发出去，服务端解出来是一个零值结构体——
// 对要求字段的方法来说，这会把“没给参数”伪装成“给了空参数”。
func rawPayload(p json.RawMessage) any {
	if len(p) == 0 {
		return nil
	}
	return p
}

// logSink 是 run 模式下的日志文件；其它子命令为空，直接写 stderr。
var logSink *agent.LogFile

// logf 是统一日志出口。
func logf(format string, args ...any) {
	if logSink != nil {
		logSink.Printf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, "[dec-agent] "+format+"\n", args...)
}

// isTerminal 报告 f 是否连着终端：前台手动运行时日志同时回显到终端。
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// fallback 返回第一个非空值。
func fallback(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
