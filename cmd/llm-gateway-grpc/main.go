package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/poly-workshop/go-webmods/app"
	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/health"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmprovider/dashscope"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmprovider/openrouter"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/server/grpcserver"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs"
	}
	app.InitWithConfigPath("llm-gateway-grpc", configPath)

	cfg, err := config.LoadGRPC()
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	providers := map[string]llmgateway.Provider{
		"dashscope": dashscope.NewProvider(
			cfg.LLM.Providers.DashScope.BaseURL,
			cfg.LLM.Providers.DashScope.APIKey,
			cfg.LLM.Providers.DashScope.Timeout,
		),
		"openrouter": openrouter.NewProvider(
			cfg.LLM.Providers.OpenRouter.BaseURL,
			cfg.LLM.Providers.OpenRouter.APIKey,
			cfg.LLM.Providers.OpenRouter.Timeout,
		),
	}

	models := make([]llmgateway.ModelSpec, 0, len(cfg.LLM.Models))
	for _, m := range cfg.LLM.Models {
		models = append(models, llmgateway.ModelSpec{
			ID:            m.ID,
			Name:          m.Name,
			Provider:      m.Provider,
			Capabilities:  m.Capabilities,
			UpstreamModel: m.UpstreamModel,
		})
	}

	// TODO: Implement a concrete GenerationRepository (e.g., in-memory or database).
	// For now, pass nil to skip generation record storage.
	appSvc := llmgateway.NewService(providers, models, nil)

	serviceTokens := make([]auth.ServiceToken, 0, len(cfg.Auth.ServiceTokens))
	for _, t := range cfg.Auth.ServiceTokens {
		serviceTokens = append(serviceTokens, auth.ServiceToken{Name: t.Name, Token: t.Token})
	}
	authMgr := auth.NewManager(serviceTokens, cfg.Auth.TempTTL)

	grpcSrv, err := grpcserver.New(cfg.GRPC.Listen, appSvc, authMgr)
	if err != nil {
		slog.Error("create grpc server failed", "error", err)
		os.Exit(1)
	}

	healthSrv := &http.Server{
		Addr: cfg.Health.Listen,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/livez":
				health.Livez(w, r)
			case "/readyz":
				health.Readyz(nil)(w, r)
			default:
				http.NotFound(w, r)
			}
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- grpcSrv.Start()
	}()
	go func() {
		slog.Info("health listening", "addr", cfg.Health.Listen)
		errCh <- healthSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(shutdownCtx)
		_ = grpcSrv.Stop(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server exited", "error", err)
			os.Exit(1)
		}
	}
}
