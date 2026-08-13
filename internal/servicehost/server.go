package servicehost

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/compat"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const defaultIdleTimeout = 30 * time.Minute

type Server struct {
	servicev1.UnimplementedDecServiceServer

	version          string
	instanceID       string
	broker           *operationBroker
	presence         *presenceTracker
	requestStop      func()
	machineMu        sync.Mutex
	repairedProjects sync.Map
}

func (s *Server) ensureProjectRepaired(projectRoot string, reporter app.Reporter) {
	if strings.TrimSpace(projectRoot) == "" {
		return
	}
	key := projectKey(projectRoot)
	if _, loaded := s.repairedProjects.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	for _, note := range compat.RepairOnStartup(projectRoot) {
		if reporter != nil {
			reporter.Emit(app.OperationEvent{
				Time:    time.Now(),
				Level:   app.EventInfo,
				Scope:   "compat",
				Message: note,
			})
		}
	}
}

func Run(ctx context.Context, version string) error {
	lock, err := service.AcquireServerLock()
	if err != nil {
		return err
	}
	defer lock.Unlock()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("监听本机服务端口失败: %w", err)
	}
	defer listener.Close()

	token, err := service.NewToken()
	if err != nil {
		return err
	}
	idleTimeout := loadIdleTimeout()
	stopRequested := make(chan struct{}, 1)
	host := &Server{
		version:    version,
		instanceID: fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMilli()),
		broker:     newOperationBroker(),
	}
	host.requestStop = func() {
		select {
		case stopRequested <- struct{}{}:
		default:
		}
	}
	host.presence = newPresenceTracker(idleTimeout, func() {
		host.requestStop()
	})

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(unaryAuth(token)),
		grpc.StreamInterceptor(streamAuth(token)),
	)
	servicev1.RegisterDecServiceServer(grpcServer, host)
	if err := service.WriteMetadata(listener.Addr().String(), token); err != nil {
		return err
	}
	defer service.RemoveMetadata()

	errCh := make(chan error, 1)
	go func() { errCh <- grpcServer.Serve(listener) }()
	select {
	case <-ctx.Done():
		grpcServer.GracefulStop()
		return ctx.Err()
	case <-stopRequested:
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

func loadIdleTimeout() time.Duration {
	cfg, err := config.LoadGlobalConfig()
	if err != nil || strings.TrimSpace(cfg.ServerIdleTimeout) == "" {
		return defaultIdleTimeout
	}
	d, err := time.ParseDuration(cfg.ServerIdleTimeout)
	if err != nil || d <= 0 {
		return defaultIdleTimeout
	}
	return d
}

func (s *Server) Ping(context.Context, *servicev1.PingRequest) (*servicev1.PingResponse, error) {
	return &servicev1.PingResponse{Version: s.version, InstanceId: s.instanceID}, nil
}

func (s *Server) Shutdown(context.Context, *servicev1.ShutdownRequest) (*servicev1.ShutdownResponse, error) {
	if s.requestStop != nil {
		go s.requestStop()
	}
	return &servicev1.ShutdownResponse{Accepted: true}, nil
}

func (s *Server) KeepAlive(stream grpc.BidiStreamingServer[servicev1.KeepAliveRequest, servicev1.KeepAliveResponse]) error {
	s.presence.connected()
	defer s.presence.disconnected()
	for {
		if _, err := stream.Recv(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := stream.Send(&servicev1.KeepAliveResponse{ServerTimeUnixMs: time.Now().UnixMilli()}); err != nil {
			return err
		}
	}
}

func (s *Server) GetActiveOperation(_ context.Context, req *servicev1.GetActiveOperationRequest) (*servicev1.GetActiveOperationResponse, error) {
	return &servicev1.GetActiveOperationResponse{Operation: s.broker.active(req.ProjectRoot)}, nil
}

func (s *Server) WatchOperation(req *servicev1.WatchOperationRequest, stream grpc.ServerStreamingServer[servicev1.WatchOperationResponse]) error {
	history, live, cancel, err := s.broker.subscribe(req.ProjectRoot, req.OperationId)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	defer cancel()
	for _, message := range history {
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	if live == nil {
		return nil
	}
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case message, ok := <-live:
			if !ok {
				return nil
			}
			if err := stream.Send(message); err != nil {
				return err
			}
		}
	}
}

func unaryAuth(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !validToken(ctx, token) {
			return nil, status.Error(codes.Unauthenticated, "invalid dec-server token")
		}
		return handler(ctx, req)
	}
}

func streamAuth(token string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !validToken(stream.Context(), token) {
			return status.Error(codes.Unauthenticated, "invalid dec-server token")
		}
		return handler(srv, stream)
	}
}

func validToken(ctx context.Context, want string) bool {
	values := metadata.ValueFromIncomingContext(ctx, service.TokenHeader)
	return len(values) == 1 && values[0] == want
}

type presenceTracker struct {
	mu      sync.Mutex
	count   int
	timeout time.Duration
	timer   *time.Timer
	stop    func()
}

func newPresenceTracker(timeout time.Duration, stop func()) *presenceTracker {
	p := &presenceTracker{timeout: timeout, stop: stop}
	p.timer = time.AfterFunc(timeout, stop)
	return p
}

func (p *presenceTracker) connected() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

func (p *presenceTracker) disconnected() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.count > 0 {
		p.count--
	}
	if p.count == 0 && p.timer == nil {
		p.timer = time.AfterFunc(p.timeout, p.stop)
	}
}

func (p *presenceTracker) setTimeout(timeout time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = timeout
	if p.count == 0 {
		if p.timer != nil {
			p.timer.Stop()
		}
		p.timer = time.AfterFunc(p.timeout, p.stop)
	}
}
