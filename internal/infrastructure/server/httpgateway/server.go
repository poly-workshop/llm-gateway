package httpgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	llmgatewayv1 "github.com/poly-workshop/llm-gateway/gen/go/llmgateway/v1"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/health"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Server struct {
	httpListen   string
	grpcTarget   string
	grpcInsecure bool
}

func New(httpListen, grpcTarget string, grpcInsecure bool) (*Server, error) {
	if httpListen == "" {
		return nil, fmt.Errorf("http listen address is empty")
	}
	if grpcTarget == "" {
		return nil, fmt.Errorf("grpc target is empty")
	}
	return &Server{httpListen: httpListen, grpcTarget: grpcTarget, grpcInsecure: grpcInsecure}, nil
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", health.Livez)
	mux.HandleFunc("/readyz", health.Readyz(health.GRPCDialReadyChecker(s.grpcTarget)))

	gw := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			k := strings.ToLower(key)
			switch k {
			case "x-service-token",
				"x-access-key-id",
				"x-signature",
				"x-timestamp",
				"x-nonce",
				"x-usage-callback",
				"x-llmgw-http-method",
				"x-llmgw-http-path",
				"x-llmgw-http-query",
				"x-llmgw-body-sha256":
				return k, true
			default:
				return runtime.DefaultHeaderMatcher(key)
			}
		}),
	)
	dialOpts := []grpc.DialOption{}
	if s.grpcInsecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		// Future: TLS/mTLS support. For now, keep it explicit so we don't accidentally dial insecure.
		return fmt.Errorf("grpc insecure=false is not supported yet")
	}
	if err := llmgatewayv1.RegisterLLMGatewayServiceHandlerFromEndpoint(ctx, gw, s.grpcTarget, dialOpts); err != nil {
		return err
	}

	// Inject HTTP signing context for gRPC-side signature verification.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only for grpc-gateway forwarded requests.
		r.Header.Set("X-LLMGW-HTTP-Method", r.Method)
		r.Header.Set("X-LLMGW-HTTP-Path", r.URL.Path)
		r.Header.Set("X-LLMGW-HTTP-Query", r.URL.RawQuery)

		// Hash body only when signature auth is attempted.
		// grpc-gateway will read the body later, so we must restore it after reading.
		hasSig := r.Header.Get("X-Access-Key-Id") != "" ||
			r.Header.Get("X-Signature") != "" ||
			r.Header.Get("X-Timestamp") != "" ||
			r.Header.Get("X-Nonce") != ""

		var sum [32]byte
		if !hasSig {
			sum = sha256.Sum256(nil)
		} else {
			const maxBody = 10 << 20 // 10MiB
			b, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
			_ = r.Body.Close()
			if err != nil || int64(len(b)) > maxBody {
				http.Error(w, "request body too large for signature verification", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			sum = sha256.Sum256(b)
		}
		r.Header.Set("X-LLMGW-Body-SHA256", hex.EncodeToString(sum[:]))

		gw.ServeHTTP(w, r)
	}))

	srv := &http.Server{
		Addr:              s.httpListen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http listening", "addr", s.httpListen, "grpc_target", s.grpcTarget)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
