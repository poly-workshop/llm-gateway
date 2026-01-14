package llmconfigstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gormclient "github.com/poly-workshop/go-webmods/gormclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GormConfig struct {
	Driver   string
	Host     string
	Port     int
	Username string
	Password string
	DbName   string
	SSLMode  string
}

type providerRecord struct {
	Provider       string `gorm:"primaryKey;size:64"`
	BaseURL        string `gorm:"size:512"`
	APIKey         string `gorm:"type:text"`
	TimeoutSeconds int64  `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (providerRecord) TableName() string { return "llm_gateway_provider_configs" }

type modelRecord struct {
	ID              string `gorm:"primaryKey;size:256"`
	Name            string `gorm:"size:512"`
	Provider        string `gorm:"size:64;not null"`
	Capabilities    string `gorm:"type:text;not null"` // json array
	UpstreamModel   string `gorm:"size:512"`
	MaxOutputTokens uint32 `gorm:"default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (modelRecord) TableName() string { return "llm_gateway_models" }

type gormStore struct {
	db *gorm.DB
}

func NewGorm(cfg GormConfig) (Store, error) {
	if cfg.Driver == "" {
		return nil, fmt.Errorf("missing gorm driver")
	}
	if cfg.DbName == "" {
		return nil, fmt.Errorf("missing gorm dbname")
	}
	db := gormclient.NewDB(gormclient.Config{
		Driver:   cfg.Driver,
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		DbName:   cfg.DbName,
		SSLMode:  cfg.SSLMode,
	})
	if err := db.AutoMigrate(&providerRecord{}, &modelRecord{}); err != nil {
		return nil, fmt.Errorf("auto-migrate llm config: %w", err)
	}
	return &gormStore{db: db}, nil
}

func (s *gormStore) Close(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s *gormStore) UpsertProviderConfig(ctx context.Context, cfg ProviderConfig) error {
	if cfg.Provider == "" {
		return nil
	}
	rec := providerRecord{
		Provider:       cfg.Provider,
		BaseURL:        cfg.BaseURL,
		APIKey:         cfg.APIKey,
		TimeoutSeconds: normalizeTimeoutSeconds(cfg.TimeoutSeconds),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}},
		DoUpdates: clause.AssignmentColumns([]string{"base_url", "api_key", "timeout_seconds", "updated_at"}),
	}).Create(&rec).Error
}

func (s *gormStore) DeleteProviderConfig(ctx context.Context, provider string) error {
	if provider == "" {
		return nil
	}
	return s.db.WithContext(ctx).Delete(&providerRecord{}, "provider = ?", provider).Error
}

func (s *gormStore) ListProviderConfigs(ctx context.Context) ([]ProviderConfigView, error) {
	var recs []providerRecord
	if err := s.db.WithContext(ctx).Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderConfigView, 0, len(recs))
	for _, r := range recs {
		out = append(out, ProviderConfigView{
			Provider:       r.Provider,
			BaseURL:        r.BaseURL,
			TimeoutSeconds: r.TimeoutSeconds,
			APIKeyPresent:  r.APIKey != "",
		})
	}
	return out, nil
}

func (s *gormStore) GetProviderConfigs(ctx context.Context) ([]ProviderConfig, error) {
	var recs []providerRecord
	if err := s.db.WithContext(ctx).Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderConfig, 0, len(recs))
	for _, r := range recs {
		out = append(out, ProviderConfig{
			Provider:       r.Provider,
			BaseURL:        r.BaseURL,
			APIKey:         r.APIKey,
			TimeoutSeconds: r.TimeoutSeconds,
		})
	}
	return out, nil
}

func (s *gormStore) UpsertModel(ctx context.Context, model ModelSpec) error {
	if model.ID == "" || model.Provider == "" {
		return nil
	}
	b, _ := json.Marshal(model.Capabilities)
	rec := modelRecord{
		ID:              model.ID,
		Name:            model.Name,
		Provider:        model.Provider,
		Capabilities:    string(b),
		UpstreamModel:   model.UpstreamModel,
		MaxOutputTokens: model.MaxOutputTokens,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "provider", "capabilities", "upstream_model", "max_output_tokens", "updated_at"}),
	}).Create(&rec).Error
}

func (s *gormStore) DeleteModel(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return s.db.WithContext(ctx).Delete(&modelRecord{}, "id = ?", id).Error
}

func (s *gormStore) ListModels(ctx context.Context) ([]ModelSpec, error) {
	var recs []modelRecord
	if err := s.db.WithContext(ctx).Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make([]ModelSpec, 0, len(recs))
	for _, r := range recs {
		var caps []string
		_ = json.Unmarshal([]byte(r.Capabilities), &caps)
		out = append(out, ModelSpec{
			ID:              r.ID,
			Name:            r.Name,
			Provider:        r.Provider,
			Capabilities:    caps,
			UpstreamModel:   r.UpstreamModel,
			MaxOutputTokens: r.MaxOutputTokens,
		})
	}
	return out, nil
}
