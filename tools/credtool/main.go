// 验收辅助：直接调用本机 dec-server 的凭据请求通道 RPC。
// 用法：
//   go run ./tools/credtool pull    — 拉取当前挂起的凭据请求描述
//   go run ./tools/credtool submit <request_id> <secret> — 模拟 Console 提交凭据
//   go run ./tools/credtool cancel <request_id> — 模拟 Console 取消
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: credtool pull|submit <id> <secret>|cancel <id>")
		os.Exit(2)
	}
	endpoint := os.Getenv("DEC_ENDPOINT")
	if endpoint == "" {
		endpoint = "127.0.0.1:47653"
	}
	version := os.Getenv("DEC_VERSION")
	if version == "" {
		version = "v1.13.103"
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-dec-client-version", version))
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer conn.Close()
	client := servicev1.NewDecServiceClient(conn)
	switch args[0] {
	case "pull":
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resp, err := client.PullCredentialRequest(cctx, &servicev1.PullCredentialRequestRequest{})
		if err != nil {
			fmt.Fprintln(os.Stderr, "pull:", err)
			os.Exit(1)
		}
		fmt.Printf("pending=%v kind=%v request_id=%q prompt=%q operation=%q\n",
			resp.GetPending(), resp.GetKind(), resp.GetRequestId(), resp.GetPrompt(), resp.GetOperation())
	case "submit":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "submit needs <request_id> <secret>")
			os.Exit(2)
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		resp, err := client.SubmitCredential(cctx, &servicev1.SubmitCredentialRequest{
			RequestId: args[1],
			Secret:    args[2],
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "submit:", err)
			os.Exit(1)
		}
		fmt.Printf("accepted=%v error=%q\n", resp.GetAccepted(), resp.GetError())
	case "cancel":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "cancel needs <request_id>")
			os.Exit(2)
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		resp, err := client.SubmitCredential(cctx, &servicev1.SubmitCredentialRequest{
			RequestId: args[1],
			Canceled:  true,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "cancel:", err)
			os.Exit(1)
		}
		fmt.Printf("accepted=%v error=%q\n", resp.GetAccepted(), resp.GetError())
	default:
		fmt.Fprintln(os.Stderr, "unknown command", args[0])
		os.Exit(2)
	}
}
