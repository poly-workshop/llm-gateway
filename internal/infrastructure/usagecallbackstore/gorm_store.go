package usagecallbackstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	gormclient "github.com/poly-workshop/go-webmods/gormclient"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
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

type gormRecord struct {
	Subject string `gorm:"primaryKey;size:256"`
	URLs    string `gorm:"type:text;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (gormRecord) TableName() string { return "llm_gateway_usage_callback_allowlists" }

type gormStore struct {
	db *gorm.DB
}

func NewGorm(cfg GormConfig) (auth.UsageCallbackStore, error) {
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
	if err := db.AutoMigrate(&gormRecord{}); err != nil {
		return nil, fmt.Errorf("auto-migrate usage callback allowlists: %w", err)
	}
	return &gormStore{db: db}, nil
}

func (s *gormStore) GetAllowlist(ctx context.Context, subject string) ([]string, error) {
	if subject == "" {
		return nil, nil
	}
	var rec gormRecord
	err := s.db.WithContext(ctx).First(&rec, "subject = ?", subject).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if rec.URLs == "" {
		return nil, nil
	}
	var urls []string
	if err := json.Unmarshal([]byte(rec.URLs), &urls); err != nil {
		return nil, fmt.Errorf("decode urls json: %w", err)
	}
	return urls, nil
}

func (s *gormStore) SetAllowlist(ctx context.Context, subject string, urls []string) error {
	if subject == "" {
		return nil
	}
	if len(urls) == 0 {
		return s.db.WithContext(ctx).Delete(&gormRecord{}, "subject = ?", subject).Error
	}
	b, err := json.Marshal(urls)
	if err != nil {
		return err
	}
	rec := gormRecord{Subject: subject, URLs: string(b)}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subject"}},
		DoUpdates: clause.AssignmentColumns([]string{"urls", "updated_at"}),
	}).Create(&rec).Error
}

func (s *gormStore) Close(ctx context.Context) error {
	sqlDB, err := s.db.WithContext(ctx).DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
