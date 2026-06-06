package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`
	ASR struct {
		Provider string `yaml:"provider"`
		Bailian  struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"bailian"`
	} `yaml:"asr"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	cfg.Server.Port = ":8080"
	cfg.ASR.Provider = "mock"

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if v := os.Getenv("DASHSCOPE_API_KEY"); v != "" {
		cfg.ASR.Bailian.APIKey = v
	}
	if v := os.Getenv("ASR_PROVIDER"); v != "" {
		cfg.ASR.Provider = v
	}

	return cfg, nil
}
