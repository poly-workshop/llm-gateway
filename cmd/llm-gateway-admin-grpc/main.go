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
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmconfigstore"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/server/adminserver"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/transport/grpcadapter"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/usagecallbackstore"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs"
	}
	app.InitWithConfigPath("llm-gateway-admin-grpc", configPath)

	cfg, err := config.LoadAdminGRPC()
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var cbStore auth.UsageCallbackStore
	switch cfg.Storage.Backend {
	case "gorm":
		cbStore, err = usagecallbackstore.NewGorm(usagecallbackstore.GormConfig{
			Driver:   cfg.Storage.Gorm.Driver,
			Host:     cfg.Storage.Gorm.Host,
			Port:     cfg.Storage.Gorm.Port,
			Username: cfg.Storage.Gorm.Username,
			Password: cfg.Storage.Gorm.Password,
			DbName:   cfg.Storage.Gorm.DbName,
			SSLMode:  cfg.Storage.Gorm.SSLMode,
		})
	case "mongodb":
		cbStore, err = usagecallbackstore.NewMongo(usagecallbackstore.MongoConfig{
			URI:        cfg.Storage.MongoDB.URI,
			Database:   cfg.Storage.MongoDB.Database,
			Collection: cfg.Storage.MongoDB.Collection,
		})
	default:
		err = fmt.Errorf("unsupported storage.backend=%q (supported: gorm, mongodb)", cfg.Storage.Backend)
	}
	if cbStore == nil && err == nil {
		err = fmt.Errorf("storage.backend must be configured (memory backend is not supported)")
	}
	if err != nil {
		slog.Error("init usage callback store failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if cbStore != nil {
			_ = cbStore.Close(context.Background())
		}
	}()

	allowlistMgr := auth.NewManager(false, nil, cbStore, cfg.Auth.UsageCallback.CacheTTL)
	defer func() { _ = allowlistMgr.Close(context.Background()) }()

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

	signer, err := auth.NewJWTSigner(
		cfg.Auth.JWTSigning.Issuer,
		cfg.Auth.JWTSigning.Audience,
		cfg.Auth.JWTSigning.PrivateKeyPEM,
		cfg.Auth.JWTSigning.DefaultTTL,
	)
	if err != nil {
		slog.Error("init jwt signer failed", "error", err)
		os.Exit(1)
	}

	adminSvc := grpcadapter.NewLLMGatewayAdminService(signer, allowlistMgr, llmStore)
	srv, err := adminserver.New(cfg.GRPC.Listen, cfg.Auth.Admin.ServiceToken, adminSvc)
	if err != nil {
		slog.Error("create admin grpc server failed", "error", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Stop(shutdownCtx)
	case err := <-errCh:
		if err != nil {
			slog.Error("admin server exited", "error", err)
			os.Exit(1)
		}
	}
}
