package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`
	Storage struct {
		Driver string `yaml:"driver"`
		DBPath string `yaml:"db_path"`
		DSN    string `yaml:"dsn"`
	} `yaml:"storage"`
	ASR struct {
		Provider string `yaml:"provider"`
		Bailian  struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"bailian"`
	} `yaml:"asr"`
	Translator struct {
		Provider string `yaml:"provider"`
		Bailian  struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"bailian"`
	} `yaml:"translator"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	cfg.Server.Port = ":8080"
	cfg.Storage.Driver = "sqlite"
	cfg.Storage.DBPath = "./tmk.db"
	cfg.ASR.Provider = "mock"
	cfg.Translator.Provider = "mock"

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if v := os.Getenv("DASHSCOPE_API_KEY"); v != "" {
		cfg.ASR.Bailian.APIKey = v
		cfg.Translator.Bailian.APIKey = v
	}
	if v := os.Getenv("ASR_PROVIDER"); v != "" {
		cfg.ASR.Provider = v
	}
	if v := os.Getenv("TRANSLATOR_PROVIDER"); v != "" {
		cfg.Translator.Provider = v
	}
	if v := os.Getenv("DB_DRIVER"); v != "" {
		cfg.Storage.Driver = v
	}
	if v := os.Getenv("DB_DSN"); v != "" {
		cfg.Storage.DSN = v
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.Storage.DBPath = v
	}
	if v := os.Getenv("SERVER_PORT"); v != "" {
		cfg.Server.Port = v
	}
	if cfg.Translator.Bailian.APIKey == "" {
		cfg.Translator.Bailian.APIKey = cfg.ASR.Bailian.APIKey
	}

	return cfg, nil
}
