package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/poly-workshop/go-webmods/grpcutils"
	llmgatewayv1 "github.com/poly-workshop/llm-gateway/gen/go/llmgateway/v1"
	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/transport/grpcadapter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	listenAddr string
	s          *grpc.Server
	lis        net.Listener
}

func New(listenAddr string, appSvc *llmgateway.Service, authMgr *auth.Manager) (*Server, error) {
	if listenAddr == "" {
		return nil, fmt.Errorf("grpc listen address is empty")
	}
	if appSvc == nil {
		return nil, fmt.Errorf("app service is nil")
	}

	unaryInts := grpc.ChainUnaryInterceptor(
		grpcutils.BuildRequestIDInterceptor(),
		grpcutils.BuildLogInterceptor(slog.Default()),
		auth.UnaryServerInterceptor(authMgr),
	)
	streamInts := grpc.ChainStreamInterceptor(
		auth.StreamServerInterceptor(authMgr),
	)

	s := grpc.NewServer(unaryInts, streamInts)

	llmgatewayv1.RegisterLLMGatewayServiceServer(s, grpcadapter.NewLLMGatewayService(appSvc, authMgr))

	reflection.Register(s)

	return &Server{listenAddr: listenAddr, s: s}, nil
}

func (srv *Server) Start() error {
	lis, err := net.Listen("tcp", srv.listenAddr)
	if err != nil {
		return err
	}
	srv.lis = lis
	slog.Info("grpc listening", "addr", srv.listenAddr)
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
