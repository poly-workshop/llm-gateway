package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/poly-workshop/go-webmods/app"
	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmconfigstore"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmprovider/dashscope"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmprovider/openrouter"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/server/httpgateway"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs"
	}
	app.InitWithConfigPath("llm-gateway-http", configPath)

	cfg, err := config.LoadHTTP()
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// TODO: Implement a concrete GenerationRepository (e.g., in-memory or database).
	// For now, pass nil to skip generation record storage.
	appSvc := llmgateway.NewService(nil, nil, nil)

	var verifier *auth.JWTVerifier
	if cfg.Auth.JWT.PublicKeyPEM != "" {
		verifier, err = auth.NewJWTVerifier(cfg.Auth.JWT.Issuer, cfg.Auth.JWT.Audience, cfg.Auth.JWT.PublicKeyPEM)
		if err != nil {
			slog.Error("init jwt verifier failed", "error", err)
			os.Exit(1)
		}
	}
	// If JWT is required but not configured, config.LoadHTTP already returns an error.

	authMgr := auth.NewManager(cfg.Auth.JWT.Required, verifier)
	defer func() { _ = authMgr.Close(context.Background()) }()

	// LLM config store (providers + models) - managed via Admin API.
	var llmStore llmconfigstore.Store
	switch cfg.Storage.Backend {
	case "gorm":
		llmStore, err = llmconfigstore.NewGorm(llmconfigstore.GormConfig{
			Driver:   cfg.Storage.Gorm.Driver,
			Host:     cfg.Storage.Gorm.Host,
			Port:     cfg.Storage.Gorm.Port,
			Username: cfg.Storage.Gorm.Username,
			Password: cfg.Storage.Gorm.Password,
			DbName:   cfg.Storage.Gorm.DbName,
			SSLMode:  cfg.Storage.Gorm.SSLMode,
		})
	case "mongodb":
		llmStore, err = llmconfigstore.NewMongo(llmconfigstore.MongoConfig{
			URI:        cfg.Storage.MongoDB.URI,
			Database:   cfg.Storage.MongoDB.Database,
			Collection: "llm_gateway",
		})
	default:
		err = fmt.Errorf("unsupported llm config backend=%q", cfg.Storage.Backend)
	}
	if err != nil {
		slog.Error("init llm config store failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = llmStore.Close(context.Background()) }()

	if err := loadAndApplyLLMConfig(ctx, appSvc, llmStore); err != nil {
		slog.Error("load llm config failed", "error", err)
		os.Exit(1)
	}
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := loadAndApplyLLMConfig(context.Background(), appSvc, llmStore); err != nil {
					slog.Warn("refresh llm config failed", "error", err)
				}
			}
		}
	}()

	srv, err := httpgateway.New(cfg.HTTP.Listen, appSvc, authMgr, cfg.HTTP.CORS)
	if err != nil {
		slog.Error("create http gateway failed", "error", err)
		os.Exit(1)
	}

	if err := srv.Start(ctx); err != nil && ctx.Err() == nil {
		slog.Error("http gateway exited", "error", err)
		os.Exit(1)
	}
}

func loadAndApplyLLMConfig(ctx context.Context, appSvc *llmgateway.Service, store llmconfigstore.Store) error {
	if appSvc == nil || store == nil {
		return fmt.Errorf("missing app service or store")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	providersCfg, err := store.GetProviderConfigs(ctx)
	if err != nil {
		return err
	}
	models, err := store.ListModels(ctx)
	if err != nil {
		return err
	}
	if len(providersCfg) == 0 || len(models) == 0 {
		return fmt.Errorf("llm config not initialized (providers=%d, models=%d)", len(providersCfg), len(models))
	}

	// Build providers.
	providers := make(map[string]llmgateway.Provider, len(providersCfg))
	for _, pc := range providersCfg {
		timeout := time.Duration(pc.TimeoutSeconds) * time.Second
		switch pc.Provider {
		case "dashscope":
			baseURL := pc.BaseURL
			if baseURL == "" {
				baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
			}
			providers["dashscope"] = dashscope.NewProvider(baseURL, pc.APIKey, timeout)
		case "openrouter":
			baseURL := pc.BaseURL
			if baseURL == "" {
				baseURL = "https://openrouter.ai/api/v1"
			}
			providers["openrouter"] = openrouter.NewProvider(baseURL, pc.APIKey, timeout)
		default:
			// Ignore unknown providers (admin should validate).
		}
	}

	// Convert models.
	specs := make([]llmgateway.ModelSpec, 0, len(models))
	for _, m := range models {
		specs = append(specs, llmgateway.ModelSpec{
			ID:              m.ID,
			Name:            m.Name,
			Provider:        m.Provider,
			Capabilities:    m.Capabilities,
			UpstreamModel:   m.UpstreamModel,
			MaxOutputTokens: m.MaxOutputTokens,
		})
	}
	appSvc.ReplaceConfig(providers, specs)
	return nil
}
