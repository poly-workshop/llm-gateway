package llmconfigstore

import (
	"context"
	"time"
)

type ProviderConfig struct {
	Provider       string
	BaseURL        string
	APIKey         string
	TimeoutSeconds int64
}

type ProviderConfigView struct {
	Provider       string
	BaseURL        string
	TimeoutSeconds int64
	APIKeyPresent  bool
}

type ModelSpec struct {
	ID              string
	Name            string
	Provider        string
	Capabilities    []string
	UpstreamModel   string
	MaxOutputTokens uint32
}

type Store interface {
	Close(ctx context.Context) error

	UpsertProviderConfig(ctx context.Context, cfg ProviderConfig) error
	DeleteProviderConfig(ctx context.Context, provider string) error
	ListProviderConfigs(ctx context.Context) ([]ProviderConfigView, error)
	GetProviderConfigs(ctx context.Context) ([]ProviderConfig, error)

	UpsertModel(ctx context.Context, model ModelSpec) error
	DeleteModel(ctx context.Context, id string) error
	ListModels(ctx context.Context) ([]ModelSpec, error)
}

func normalizeTimeoutSeconds(s int64) int64 {
	if s <= 0 {
		return int64((30 * time.Second).Seconds())
	}
	return s
}
