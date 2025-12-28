package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/poly-workshop/go-webmods/app"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
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

	srv, err := httpgateway.New(cfg.HTTP.Listen, cfg.GRPC.Target, cfg.GRPC.Insecure)
	if err != nil {
		slog.Error("create http gateway failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil && ctx.Err() == nil {
		slog.Error("http gateway exited", "error", err)
		os.Exit(1)
	}
}
