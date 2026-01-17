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

	// Storage config is shared by multiple modules (e.g. LLM config store).
	Storage StorageConfig `mapstructure:"storage"`

	// UsageSink config for publishing usage events.
	UsageSink UsageSinkConfig `mapstructure:"usage_sink"`

	Auth struct {
		JWT struct {
			Required      bool   `mapstructure:"required"`
			Issuer        string `mapstructure:"issuer"`
			Audience      string `mapstructure:"audience"`
			PublicKeyFile string `mapstructure:"public_key_file"`
			PublicKeyPEM  string `mapstructure:"-"`
		} `mapstructure:"jwt"`
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

	// Validate usage sink config
	if cfg.UsageSink.Enabled {
		if cfg.UsageSink.Backend == "" {
			return cfg, fmt.Errorf("usage_sink.backend is required when usage_sink.enabled is true")
		}
		if cfg.UsageSink.Backend != "redis_stream" {
			return cfg, fmt.Errorf("invalid usage_sink.backend: %q (supported: redis_stream)", cfg.UsageSink.Backend)
		}
	}

	return cfg, nil
}

type AdminGRPCAppConfig struct {
	GRPC struct {
		Listen string `mapstructure:"listen"`
	} `mapstructure:"grpc"`

	// Storage config is shared by multiple modules (e.g. LLM config store).
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
	if cfg.Auth.JWTSigning.DefaultTTL == 0 {
		cfg.Auth.JWTSigning.DefaultTTL = 15 * time.Minute
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

type UsageSinkConfig struct {
	// Enabled controls whether usage events are published.
	Enabled bool `mapstructure:"enabled"`

	// Backend specifies the sink type ("redis_stream" or empty to disable).
	Backend string `mapstructure:"backend"`

	// RedisStream config for Redis Stream sink.
	RedisStream struct {
		Addr       string        `mapstructure:"addr"`
		Password   string        `mapstructure:"password"`
		StreamKey  string        `mapstructure:"stream_key"`
		MaxLen     int64         `mapstructure:"max_len"`
		Timeout    time.Duration `mapstructure:"timeout"`
		ApproxTrim bool          `mapstructure:"approx_trim"`
	} `mapstructure:"redis_stream"`
}

func unmarshalViper(v *viper.Viper, out any) error {
	if err := v.Unmarshal(out); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}
