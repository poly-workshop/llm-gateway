package config

import (
	"fmt"
	"time"

	"github.com/poly-workshop/go-webmods/app"
	"github.com/spf13/viper"
)

type GRPCAppConfig struct {
	GRPC struct {
		Listen string `mapstructure:"listen"`
	} `mapstructure:"grpc"`

	Health struct {
		Listen string `mapstructure:"listen"`
	} `mapstructure:"health"`

	Auth struct {
		TempTTL       time.Duration `mapstructure:"temp_ttl"`
		ServiceTokens []struct {
			Name  string `mapstructure:"name"`
			Token string `mapstructure:"token"`
		} `mapstructure:"service_tokens"`
	} `mapstructure:"auth"`

	LLM struct {
		Providers struct {
			DashScope struct {
				BaseURL string        `mapstructure:"base_url"`
				APIKey  string        `mapstructure:"api_key"`
				Timeout time.Duration `mapstructure:"timeout"`
			} `mapstructure:"dashscope"`
			OpenRouter struct {
				BaseURL string        `mapstructure:"base_url"`
				APIKey  string        `mapstructure:"api_key"`
				Timeout time.Duration `mapstructure:"timeout"`
			} `mapstructure:"openrouter"`
		} `mapstructure:"providers"`

		Models []struct {
			ID            string   `mapstructure:"id"`
			Name          string   `mapstructure:"name"`
			Provider      string   `mapstructure:"provider"`
			Capabilities  []string `mapstructure:"capabilities"`
			UpstreamModel string   `mapstructure:"upstream_model"`
		} `mapstructure:"models"`
	} `mapstructure:"llm"`
}

func LoadGRPC() (GRPCAppConfig, error) {
	cfg := GRPCAppConfig{}

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
	if cfg.Health.Listen == "" {
		return cfg, fmt.Errorf("missing config: health.listen")
	}
	if cfg.LLM.Providers.DashScope.BaseURL == "" {
		cfg.LLM.Providers.DashScope.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	if cfg.LLM.Providers.OpenRouter.BaseURL == "" {
		cfg.LLM.Providers.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.Auth.TempTTL == 0 {
		cfg.Auth.TempTTL = 15 * time.Minute
	}

	return cfg, nil
}

type HTTPAppConfig struct {
	HTTP struct {
		Listen string `mapstructure:"listen"`
	} `mapstructure:"http"`

	GRPC struct {
		Target   string `mapstructure:"target"`
		Insecure bool   `mapstructure:"insecure"`
	} `mapstructure:"grpc"`
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
	if cfg.GRPC.Target == "" {
		return cfg, fmt.Errorf("missing config: grpc.target")
	}

	return cfg, nil
}

func unmarshalViper(v *viper.Viper, out any) error {
	if err := v.Unmarshal(out); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}
