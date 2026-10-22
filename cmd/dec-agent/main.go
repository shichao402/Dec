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

	"github.com/shichao402/Dec/internal/agent"
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
	case "status":
		err = cmdStatus(args)
	case "projects":
		err = cmdProjects(args)
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
  dec-agent recover      在 CLI 里恢复（服务端进入恢复模式时）
  dec-agent status       打印当前状态
  dec-agent projects     列出本机项目登记
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
	port := fs.Int("port", 8787, "本机页面端口")
	name := fs.String("name", "", "上报给服务端的设备名")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := mustState()
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

	// 本机页面与管理面转发（需求 2.10、13.1）。
	loop := agent.NewLoopback(agent.LoopbackOptions{
		Port:   *port,
		Logf:   logf,
		Status: syncer.Status,
		Call: func(r *http.Request, m, src string, payload json.RawMessage) (json.RawMessage, error) {
			return callRemote(ctx, syncer.Client(), proto.Method(m), proto.Source(src), payload)
		},
		Enroll: func(r *http.Request, in agent.LoopbackEnrollInput) (*agent.EnrollResult, error) {
			return agent.Enroll(ctx, syncer.Client(), store, agent.EnrollInput{
				Email: in.Email, Password: in.Password, TOTP: in.TOTP, DeviceName: in.Device,
			})
		},
		Recover: func(r *http.Request, in agent.LoopbackEnrollInput) (*agent.EnrollResult, error) {
			return agent.Recover(ctx, syncer.Client(), store, agent.EnrollInput{
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
	})
	addr, err := loop.Start()
	if err != nil {
		return err
	}
	defer loop.Close()
	logf("代理已启动，版本 %s，服务地址 %s，本机页面 http://%s", Version, cfg.ServerURL, addr)

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
		Email: *email, Password: pw, TOTP: *totp, DeviceName: *name,
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
	res, err := agent.Enroll(ctx, client, store, agent.EnrollInput{
		Email: *email, Password: pw, TOTP: *totp, DeviceName: *name, Kind: *kind,
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
	res, err := agent.Recover(ctx, client, store, agent.EnrollInput{
		Email: *email, Password: pw, TOTP: *totp, DeviceName: *name,
	})
	if err != nil {
		return err
	}
	fmt.Printf("恢复成功：设备 %s，证书到期 %s\n", res.DeviceID, res.NotAfter.Format(time.RFC3339))
	return nil
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

// cmdInstall 注册系统服务。
func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	bin := fs.String("binary", "", "代理可执行文件路径，默认取当前进程")
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
	return nil
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

// logf 是统一日志出口。
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[dec-agent] "+format+"\n", args...)
}

// fallback 返回第一个非空值。
func fallback(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
