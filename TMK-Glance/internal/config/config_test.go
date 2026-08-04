package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`server:
  port: ":8080"
storage:
  driver: sqlite
  db_path: ./tmk.db
asr:
  provider: mock
translator:
  provider: mock
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("SERVER_PORT", ":18080")
	t.Setenv("DB_DRIVER", "mysql")
	t.Setenv("DB_DSN", "tmk_test:secret@tcp(localhost:3306)/tmk_test")
	t.Setenv("ASR_PROVIDER", "bailian")
	t.Setenv("TRANSLATOR_PROVIDER", "bailian")
	t.Setenv("DASHSCOPE_API_KEY", "test-key")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Port != ":18080" {
		t.Fatalf("server port = %q, want %q", cfg.Server.Port, ":18080")
	}
	if cfg.Storage.Driver != "mysql" || cfg.Storage.DSN == "" {
		t.Fatalf("storage override not applied: driver=%q dsn=%q", cfg.Storage.Driver, cfg.Storage.DSN)
	}
	if cfg.ASR.Bailian.APIKey != "test-key" || cfg.Translator.Bailian.APIKey != "test-key" {
		t.Fatal("provider API key override not applied")
	}
}
