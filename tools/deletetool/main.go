// 验收辅助：绕过 MCP 层字段映射缺陷，直连 dec-server RunOperation 执行 delete。
// MCP dec_delete 的 agenttools 映射只传 Name 不传 SSHKeyName（kind=ssh 报"SSH Key 名称不能为空"），
// 本工具在 payload 里手工带 SSHKeyName 走与 Console 相同的 RunOperation delete 通道。
// 用法：deletetool <plane> <kind> <sshkey-name|secret-path> <secrets-bundle>
//   deletetool global ssh .sshkey/cvm-a1 tencent-cloud/private/global
//   deletetool global secret .password/cvm-gz tencent-cloud/private/global
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: deletetool <plane> <kind> <name> <secrets_bundle> [project_root]")
		os.Exit(2)
	}
	plane, kind, name, bundle := args[0], args[1], args[2], args[3]
	root := "D:/workspace/GitHub/Dec"
	if len(args) > 4 {
		root = args[4]
	}
	endpoint := os.Getenv("DEC_ENDPOINT")
	if endpoint == "" {
		endpoint = "127.0.0.1:47653"
	}
	token := os.Getenv("DEC_TOKEN")
	if token == "" {
		runJSON, err := os.ReadFile(os.Getenv("USERPROFILE") + "/.dec/run/server.json")
		if err == nil {
			var run struct {
				Endpoint string `json:"endpoint"`
				Token    string `json:"token"`
			}
			if json.Unmarshal(runJSON, &run) == nil {
				if run.Token != "" {
					token = run.Token
				}
				if os.Getenv("DEC_ENDPOINT") == "" && run.Endpoint != "" {
					endpoint = run.Endpoint
				}
			}
		}
	}
	version := os.Getenv("DEC_VERSION")
	if version == "" {
		version = "v1.13.103"
	}

	item := map[string]any{
		"Kind":          kind,
		"SecretsBundle": bundle,
		"Partition":     "remote",
	}
	switch kind {
	case "ssh":
		item["SSHKeyName"] = name
		item["DecBundleName"] = strings.SplitN(bundle, "/", 2)[0]
	case "secret":
		item["SecretPath"] = name
	default:
		fmt.Fprintln(os.Stderr, "kind must be ssh|secret")
		os.Exit(2)
	}
	payload, err := json.Marshal(map[string]any{
		"Items":     []map[string]any{item},
		"Confirmed": true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-dec-client-version", version,
		"x-dec-token", token,
	))
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer conn.Close()
	client := servicev1.NewDecServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	stream, err := client.RunOperation(cctx, &servicev1.RunOperationRequest{
		Operation:      "delete",
		ProjectRoot:    root,
		PayloadJson:    payload,
		WorkspacePlane: plane,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
		os.Exit(1)
	}
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "recv:", err)
			os.Exit(1)
		}
		if ev := resp.GetEvent(); ev != nil {
			fmt.Printf("[event] %s %s\n", ev.GetScope(), ev.GetMessage())
		}
		if resp.GetDone() {
			if e := resp.GetError(); e != "" {
				fmt.Println("error:", e)
				os.Exit(1)
			}
			var pretty json.RawMessage
			if len(resp.GetResultJson()) > 0 {
				pretty = json.RawMessage(resp.GetResultJson())
				var buf strings.Builder
			_ = pretty
				_ = buf
				fmt.Println("result:", string(resp.GetResultJson()))
			}
			fmt.Println("DONE")
			break
		}
	}
}
