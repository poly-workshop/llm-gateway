package adminserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/poly-workshop/go-webmods/grpcutils"
	adminv1 "github.com/poly-workshop/llm-gateway/gen/go/llmgateway/admin/v1"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	listenAddr string
	s          *grpc.Server
	lis        net.Listener
}

func New(listenAddr string, adminToken string, svc adminv1.LLMGatewayAdminServiceServer) (*Server, error) {
	if listenAddr == "" {
		return nil, fmt.Errorf("grpc listen address is empty")
	}
	if svc == nil {
		return nil, fmt.Errorf("admin service is nil")
	}

	unaryInts := grpc.ChainUnaryInterceptor(
		grpcutils.BuildRequestIDInterceptor(),
		grpcutils.BuildLogInterceptor(slog.Default()),
		auth.AdminUnaryServerInterceptor(adminToken),
	)
	streamInts := grpc.ChainStreamInterceptor(
		auth.AdminStreamServerInterceptor(adminToken),
	)

	s := grpc.NewServer(unaryInts, streamInts)
	adminv1.RegisterLLMGatewayAdminServiceServer(s, svc)

	// Keep reflection enabled for dev / grpcurl usage.
	reflection.Register(s)

	return &Server{listenAddr: listenAddr, s: s}, nil
}

func (srv *Server) Start() error {
	lis, err := net.Listen("tcp", srv.listenAddr)
	if err != nil {
		return err
	}
	srv.lis = lis
	slog.Info("admin grpc listening", "addr", srv.listenAddr)
	return srv.s.Serve(lis)
}

func (srv *Server) Stop(ctx context.Context) error {
	if srv.s == nil {
		return nil
	}

	done := make(chan struct{})
	go func() {
		srv.s.GracefulStop()
		close(done)
	}()

	select {
	case <-ctx.Done():
		srv.s.Stop()
		return ctx.Err()
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		srv.s.Stop()
		return fmt.Errorf("grpc graceful stop timed out")
	}
}
