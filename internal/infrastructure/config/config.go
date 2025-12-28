package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/poly-workshop/go-webmods/app"
	"github.com/spf13/viper"
)

type HTTPAppConfig struct {
	HTTP struct {
		Listen string     `mapstructure:"listen"`
		CORS   CORSConfig `mapstructure:"cors"`
	} `mapstructure:"http"`

	// Storage config is shared by multiple modules (e.g. LLM config store, usage callback allowlists).
	//
	// New config key: [storage]
	// Legacy compatibility: falls back to [auth.usage_callback] if [storage] is not set.
	Storage StorageConfig `mapstructure:"storage"`

	Auth struct {
		JWT struct {
			Required      bool   `mapstructure:"required"`
			Issuer        string `mapstructure:"issuer"`
			Audience      string `mapstructure:"audience"`
			PublicKeyFile string `mapstructure:"public_key_file"`
			PublicKeyPEM  string `mapstructure:"-"`
		} `mapstructure:"jwt"`

		// UsageCallback is NOT a storage config; it only holds usage-callback-specific knobs.
		//
		// Legacy note: historically [auth.usage_callback] also carried shared storage settings.
		// We still unmarshal those legacy fields into this struct, then copy them into cfg.Storage
		// if [storage] is not configured.
		UsageCallback LegacyUsageCallbackConfig `mapstructure:"usage_callback"`
	} `mapstructure:"auth"`
}

type CORSConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	AllowOrigins     []string      `mapstructure:"allow_origins"`
	AllowMethods     []string      `mapstructure:"allow_methods"`
	AllowHeaders     []string      `mapstructure:"allow_headers"`
	ExposeHeaders    []string      `mapstructure:"expose_headers"`
	AllowCredentials bool          `mapstructure:"allow_credentials"`
	MaxAge           time.Duration `mapstructure:"max_age"`
}

func LoadHTTP() (HTTPAppConfig, error) {
	cfg := HTTPAppConfig{}

	v := app.Config()
	if v == nil {
		return cfg, fmt.Errorf("app.Config() is nil: did you call app.Init(...) first?")
	}

	if err := unmarshalViper(v, &cfg); err != nil {
		return cfg, err
	}

	if cfg.HTTP.Listen == "" {
		return cfg, fmt.Errorf("missing config: http.listen")
	}
	if cfg.Auth.JWT.PublicKeyFile != "" {
		pemText, err := readPEMFile(cfg.Auth.JWT.PublicKeyFile)
		if err != nil {
			return cfg, fmt.Errorf("read auth.jwt.public_key_file: %w", err)
		}
		cfg.Auth.JWT.PublicKeyPEM = pemText
	}
	if cfg.Auth.JWT.Required && cfg.Auth.JWT.PublicKeyPEM == "" {
		return cfg, fmt.Errorf("jwt required but not configured (missing auth.jwt.public_key_file)")
	}

	// Defaults for browser-friendly CORS.
	if cfg.HTTP.CORS.MaxAge == 0 {
		cfg.HTTP.CORS.MaxAge = 10 * time.Minute
	}
	normalizeStorageConfig(&cfg.Storage, &cfg.Auth.UsageCallback)
	if cfg.Auth.UsageCallback.CacheTTL == 0 {
		cfg.Auth.UsageCallback.CacheTTL = 30 * time.Second
	}

	return cfg, nil
}

type AdminGRPCAppConfig struct {
	GRPC struct {
		Listen string `mapstructure:"listen"`
	} `mapstructure:"grpc"`

	// Storage config is shared by multiple modules (e.g. LLM config store, usage callback allowlists).
	//
	// New config key: [storage]
	// Legacy compatibility: falls back to [auth.usage_callback] if [storage] is not set.
	Storage StorageConfig `mapstructure:"storage"`

	Auth struct {
		Admin struct {
			// Single admin token, provided as incoming gRPC metadata "x-service-token".
			ServiceToken string `mapstructure:"service_token"`
		} `mapstructure:"admin"`

		JWTSigning struct {
			Issuer         string        `mapstructure:"issuer"`
			Audience       string        `mapstructure:"audience"`
			PrivateKeyFile string        `mapstructure:"private_key_file"`
			PrivateKeyPEM  string        `mapstructure:"-"`
			DefaultTTL     time.Duration `mapstructure:"default_ttl"`
		} `mapstructure:"jwt_signing"`

		// UsageCallback is NOT a storage config; it only holds usage-callback-specific knobs.
		//
		// Legacy note: historically [auth.usage_callback] also carried shared storage settings.
		// We still unmarshal those legacy fields into this struct, then copy them into cfg.Storage
		// if [storage] is not configured.
		UsageCallback LegacyUsageCallbackConfig `mapstructure:"usage_callback"`
	} `mapstructure:"auth"`
}

func LoadAdminGRPC() (AdminGRPCAppConfig, error) {
	cfg := AdminGRPCAppConfig{}

	v := app.Config()
	if v == nil {
		return cfg, fmt.Errorf("app.Config() is nil: did you call app.Init(...) first?")
	}

	if err := unmarshalViper(v, &cfg); err != nil {
		return cfg, err
	}

	if cfg.GRPC.Listen == "" {
		return cfg, fmt.Errorf("missing config: grpc.listen")
	}
	if cfg.Auth.JWTSigning.PrivateKeyFile == "" {
		return cfg, fmt.Errorf("missing config: auth.jwt_signing.private_key_file")
	}
	pemText, err := readPEMFile(cfg.Auth.JWTSigning.PrivateKeyFile)
	if err != nil {
		return cfg, fmt.Errorf("read auth.jwt_signing.private_key_file: %w", err)
	}
	cfg.Auth.JWTSigning.PrivateKeyPEM = pemText
	normalizeStorageConfig(&cfg.Storage, &cfg.Auth.UsageCallback)
	if cfg.Auth.JWTSigning.DefaultTTL == 0 {
		cfg.Auth.JWTSigning.DefaultTTL = 15 * time.Minute
	}
	if cfg.Auth.UsageCallback.CacheTTL == 0 {
		cfg.Auth.UsageCallback.CacheTTL = 30 * time.Second
	}
	return cfg, nil
}

func readPEMFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pemText := strings.TrimSpace(string(b))
	if pemText == "" {
		return "", fmt.Errorf("file is empty")
	}
	// Ensure trailing newline for PEM decoders that expect it.
	if !strings.HasSuffix(pemText, "\n") {
		pemText += "\n"
	}
	return pemText, nil
}

type StorageConfig struct {
	Backend string `mapstructure:"backend"`

	Gorm struct {
		Driver   string `mapstructure:"driver"`
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
		DbName   string `mapstructure:"dbname"`
		SSLMode  string `mapstructure:"sslmode"`
	} `mapstructure:"gorm"`

	MongoDB struct {
		URI      string `mapstructure:"uri"`
		Database string `mapstructure:"database"`
		// Collection is optional; individual modules may override it.
		Collection string `mapstructure:"collection"`
	} `mapstructure:"mongodb"`
}

// LegacyUsageCallbackConfig represents the historical [auth.usage_callback] schema.
// It included both usage-callback-specific settings and shared storage backend settings.
type LegacyUsageCallbackConfig struct {
	Backend  string        `mapstructure:"backend"`
	CacheTTL time.Duration `mapstructure:"cache_ttl"`

	Gorm struct {
		Driver   string `mapstructure:"driver"`
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
		DbName   string `mapstructure:"dbname"`
		SSLMode  string `mapstructure:"sslmode"`
	} `mapstructure:"gorm"`

	MongoDB struct {
		URI        string `mapstructure:"uri"`
		Database   string `mapstructure:"database"`
		Collection string `mapstructure:"collection"`
	} `mapstructure:"mongodb"`
}

func normalizeStorageConfig(storage *StorageConfig, legacy *LegacyUsageCallbackConfig) {
	if storage == nil || legacy == nil {
		return
	}
	if storage.Backend != "" {
		return
	}
	// Backward compat: if [storage] is missing, reuse legacy [auth.usage_callback] backend settings.
	storage.Backend = legacy.Backend
	storage.Gorm = legacy.Gorm
	storage.MongoDB = legacy.MongoDB
}

func unmarshalViper(v *viper.Viper, out any) error {
	if err := v.Unmarshal(out); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}
