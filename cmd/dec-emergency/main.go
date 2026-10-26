// Command dec-emergency 是服务器本机的应急管理命令（需求 18.12）。
//
// 单独一个二进制而不是 dec-cloud 的子命令，理由有两条：
//
//  1. dec-cloud 是常驻服务端程序，它的 main 里全是启动参数。
//     把应急命令塞进去会让「跑服务」和「改数据」混在同一个入口，
//     运维时容易看错、也容易在服务用户下误跑。
//  2. 应急场景要求它能在服务停掉时独立可用。独立二进制没有依赖，
//     拷过去就能跑，不必关心服务当前是不是在运行。
//
// 它只在服务器本机使用，不出现在任何日常文档的推荐流程里。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/shichao402/Dec/internal/cloud/emergency"
)

var (
	// Version 与 BuildTime 由 CI 通过 -ldflags 注入。
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
	case "devices":
		err = cmdDevices(args)
	case "revoke":
		err = cmdRevoke(args)
	case "issue-cert":
		err = cmdIssueCert(args)
	case "version":
		fmt.Printf("dec-emergency %s (%s)\n", Version, BuildTime)
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
	fmt.Fprint(os.Stderr, `Dec 服务器本机应急命令

  dec-emergency devices                    列出全部设备
  dec-emergency revoke     <设备ID> [原因]  吊销一台设备
  dec-emergency issue-cert <设备ID> <公钥文件> [设备名]
                                           为指定公钥直接签发证书（只接收设备）
  dec-emergency version                    打印版本

这是最后手段，仅在 Bitwarden 也不可用时使用。
日常请用管理面吊销设备、用 dec-agent enroll 登记新设备。

数据目录默认 /var/lib/dec-cloud，可用 -data-dir 覆盖。
`)
}

// commonFlags 解析各子命令共用的参数。
func commonFlags(name string, args []string) (dataDir string, rest []string, err error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dir := fs.String("data-dir", defaultDataDir(), "数据目录")
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	return *dir, fs.Args(), nil
}

// defaultDataDir 与 dec-cloud 保持一致：
// 生产部署由 systemd 指定 /var/lib/dec-cloud。
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

// openEnv 打开数据目录并挂好关闭。
func openEnv(dir string) (*emergency.Env, error) {
	return emergency.Open(dir)
}

// cmdDevices 列出设备。
//
// 「先看清现状」是应急操作的第一步：吊销前必须确认 ID 没抄错，
// 签证书前必须确认这个 ID 没被占用。
func cmdDevices(args []string) error {
	dir, _, err := commonFlags("devices", args)
	if err != nil {
		return err
	}
	env, err := openEnv(dir)
	if err != nil {
		return err
	}
	defer env.Close()

	devices, err := env.ListDevices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("没有任何设备登记。")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "设备ID\t名称\t类型\t状态\t证书到期\t最后在线")
	for _, d := range devices {
		status := "正常"
		if d.Revoked {
			status = "已吊销"
		}
		lastSeen := "从未"
		if d.LastSeen != nil {
			lastSeen = d.LastSeen.UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.ID, d.Name, d.Kind, status,
			d.CertNotAfter.UTC().Format("2006-01-02 15:04"), lastSeen)
	}
	return w.Flush()
}

// cmdRevoke 吊销设备。
func cmdRevoke(args []string) error {
	dir, rest, err := commonFlags("revoke", args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("用法: dec-emergency revoke <设备ID> [原因]")
	}
	deviceID := rest[0]
	reason := ""
	if len(rest) > 1 {
		reason = rest[1]
	}

	env, err := openEnv(dir)
	if err != nil {
		return err
	}
	defer env.Close()

	dev, err := env.RevokeDevice(deviceID, reason)
	if err != nil {
		return err
	}
	fmt.Printf("已吊销设备 %s（%s）于 %s\n", dev.ID, dev.Name, formatTimePtr(dev.RevokedAt))
	fmt.Println("吊销立即生效：它此后的任何请求都会被拒绝。")
	fmt.Println("注意：这台设备曾经拿到过的密钥需要你自行换新值，管理面的轮换清单会列出来。")
	return nil
}

// cmdIssueCert 为指定公钥签发证书。
func cmdIssueCert(args []string) error {
	dir, rest, err := commonFlags("issue-cert", args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("用法: dec-emergency issue-cert <设备ID> <公钥文件> [设备名]")
	}
	deviceID := rest[0]
	pubPath := rest[1]
	name := ""
	if len(rest) > 2 {
		name = rest[2]
	}

	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return fmt.Errorf("读取公钥文件失败: %w", err)
	}

	env, err := openEnv(dir)
	if err != nil {
		return err
	}
	defer env.Close()

	issued, dev, err := env.IssueCert(deviceID, string(pubPEM), name)
	if err != nil {
		return err
	}

	fmt.Printf("已为设备 %s（%s）签发证书，到期 %s\n",
		dev.ID, dev.Name, issued.NotAfter.UTC().Format(time.RFC3339))
	fmt.Println("设备类型为「只接收设备」：能接收下发，但不能写资产、不能看明文、不能管理设备。")
	fmt.Println("需要管理能力时，请用这台设备连接后走正常的 dec-agent enroll 流程。")
	fmt.Println()
	fmt.Println("--- 设备证书 ---")
	fmt.Print(string(issued.CertPEM))
	return nil
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "未知时间"
	}
	return t.UTC().Format(time.RFC3339)
}
