package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const TokenHeader = "x-dec-token"

type Client struct {
	conn          *grpc.ClientConn
	rpc           servicev1.DecServiceClient
	token         string
	clientVersion string
	serverVersion string
	cancel        context.CancelFunc
	once          sync.Once
}

func Connect(ctx context.Context, facade, clientID, clientVersion string) (*Client, error) {
	client, err := connectExisting(ctx, clientVersion)
	if err == nil {
		client.maybeStartPresence(facade, clientID)
		return client, nil
	}
	if err := startServerProcess(); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		client, lastErr = connectExisting(ctx, clientVersion)
		if lastErr == nil {
			client.maybeStartPresence(facade, clientID)
			return client, nil
		}
	}
	return nil, fmt.Errorf("dec-server 启动后未就绪: %w", lastErr)
}

// facadeHoldsPresence 决定门面是否持有长连 KeepAlive。
// 只有 TUI 这类「开着就应保活」的交互门面持有；MCP / CLI 是薄门面，
// 只在具体 RPC 执行期间由服务端拦截器计入 presence，调用结束即释放，
// 从而不会在进程变孤儿后长期拖住 dec-server 不空闲退出。
func facadeHoldsPresence(facade string) bool {
	return facade == "tui"
}

func (c *Client) maybeStartPresence(facade, clientID string) {
	if !facadeHoldsPresence(facade) {
		return
	}
	c.startPresence(facade, clientID)
}

func connectExisting(ctx context.Context, clientVersion string) (*Client, error) {
	meta, err := ReadMetadata()
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(meta.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(clientUnaryToken(meta.Token)),
		grpc.WithStreamInterceptor(clientStreamToken(meta.Token)),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:          conn,
		rpc:           servicev1.NewDecServiceClient(conn),
		token:         meta.Token,
		clientVersion: strings.TrimSpace(clientVersion),
	}
	pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	pong, err := client.rpc.Ping(pingCtx, &servicev1.PingRequest{})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	client.serverVersion = strings.TrimSpace(pong.GetVersion())
	return client, nil
}

func (c *Client) RPC() servicev1.DecServiceClient { return c.rpc }

func (c *Client) ClientVersion() string { return c.clientVersion }

func (c *Client) ServerVersion() string { return c.serverVersion }

// VersionMismatch 在客户端与服务端版本字符串不一致时返回 true。
// 双方同为 "dev" 时无法区分新旧二进制，返回 false（依赖 Settings 手动重启）。
func (c *Client) VersionMismatch() bool {
	return VersionsMismatch(c.clientVersion, c.serverVersion)
}

// VersionsMismatch 比较门面与 dec-server 的版本字符串。
func VersionsMismatch(clientVersion, serverVersion string) bool {
	client := normalizeVersion(clientVersion)
	server := normalizeVersion(serverVersion)
	if client == "" || server == "" {
		return false
	}
	if client == "dev" && server == "dev" {
		return false
	}
	return client != server
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return strings.ToLower(v)
}

func (c *Client) Close() error {
	var err error
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		err = c.conn.Close()
	})
	return err
}

const (
	keepAliveMinBackoff = time.Second
	keepAliveMaxBackoff = 30 * time.Second
)

func (c *Client) startPresence(facade, clientID string) {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go func() {
		backoff := keepAliveMinBackoff
		for ctx.Err() == nil {
			if c.runKeepAliveOnce(ctx, facade, clientID) {
				// 成功建立过流：重置退避，立即重连。
				backoff = keepAliveMinBackoff
				continue
			}
			// 建流 / 首发失败：退避后重试，避免空转烧 CPU。
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > keepAliveMaxBackoff {
				backoff = keepAliveMaxBackoff
			}
		}
	}()
}

// runKeepAliveOnce 建立一条 KeepAlive 流并持续接收，直到出错或 ctx 取消。
// 返回 true 表示流曾成功建立（调用方可立即重连并重置退避）；false 表示建流/首发失败。
func (c *Client) runKeepAliveOnce(ctx context.Context, facade, clientID string) (established bool) {
	stream, err := c.rpc.KeepAlive(ctx)
	if err != nil {
		return false
	}
	if err := stream.Send(&servicev1.KeepAliveRequest{ClientId: clientID, Facade: facade}); err != nil {
		return false
	}
	for ctx.Err() == nil {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	return true
}

func clientUnaryToken(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, TokenHeader, token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func clientStreamToken(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, TokenHeader, token)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

func startServerProcess() error {
	path, err := siblingExecutable("dec-server")
	if err != nil {
		return err
	}
	cmd := exec.Command(path)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = detachedProcessAttributes()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 dec-server 失败: %w", err)
	}
	return cmd.Process.Release()
}

func siblingExecutable(name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	current, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(current), name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("未找到 %s（应与 %s 位于同一目录）", name, filepath.Base(current))
	}
	return path, nil
}
