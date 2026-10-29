// release-sign 在 CI 里给发布清单签名（ADR 0058）。
//
//	DEC_RELEASE_SIGNING_KEY=<base64 种子> go run ./tools/release-sign dist/release.json
//
// 写出 dist/release.json.sig，随后立刻用代理内置的信任公钥回验一遍。
// 回验这一步是关键：Secret 里的私钥如果被换掉、贴错，签名本身照样能生成，
// 但所有代理都会拒绝它——自动更新会静默失效，直到有人发现设备都停在旧版本。
// 在 CI 里失败比在每台设备上失败好得多。
package main

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/internal/release"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release-sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("用法: release-sign <release.json>")
	}
	key := os.Getenv("DEC_RELEASE_SIGNING_KEY")
	if key == "" {
		return fmt.Errorf("缺少环境变量 DEC_RELEASE_SIGNING_KEY")
	}
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sig, err := release.Sign(key, data)
	if err != nil {
		return err
	}
	if _, err := release.ParseVerified(release.TrustedKeys, data, sig); err != nil {
		return fmt.Errorf("签名无法被代理内置公钥校验（私钥与 internal/release.TrustedKeys 不配对？）: %w", err)
	}
	if err := os.WriteFile(path+".sig", []byte(sig), 0o644); err != nil {
		return err
	}
	fmt.Printf("已签名 %s -> %s.sig\n", path, path)
	return nil
}
