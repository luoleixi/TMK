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
  segmenter:
    enabled: false
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
	t.Setenv("ASR_MAX_SENTENCE_SILENCE_MS", "750")
	t.Setenv("TRANSLATOR_PROVIDER", "bailian")
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("AUTH_ACCESS_TOKEN_TTL_MINUTES", "20")
	t.Setenv("AUTH_REFRESH_TOKEN_TTL_DAYS", "45")
	t.Setenv("AUTH_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("AUTH_BOOTSTRAP_ADMIN_PASSWORD", "test-bootstrap-password")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://admin.example.com, https://ops.example.com")
	t.Setenv("OBJECT_STORAGE_DRIVER", "local")
	t.Setenv("OBJECT_STORAGE_ROOT", "/srv/tmk/objects")
	t.Setenv("OBJECT_STORAGE_MAX_AUDIO_BYTES", "123456")
	t.Setenv("OBJECT_STORAGE_TOTAL_QUOTA_BYTES", "987654")
	t.Setenv("EVALUATION_WORKERS", "3")
	t.Setenv("EVALUATION_ITEM_TIMEOUT_SECONDS", "120")

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
	if cfg.ASR.Bailian.MaxSentenceSilenceMS != 750 {
		t.Fatalf("max sentence silence = %d, want 750", cfg.ASR.Bailian.MaxSentenceSilenceMS)
	}
	if cfg.ASR.Segmenter.Enabled || cfg.ASR.Segmenter.MaxRunes != 40 || cfg.ASR.Segmenter.MaxDurationMS != 5000 || cfg.ASR.Segmenter.SoftCommitDelayMS != 300 {
		t.Fatalf("unexpected segmenter defaults: %+v", cfg.ASR.Segmenter)
	}
	if cfg.Auth.AccessTokenTTLMinutes != 20 || cfg.Auth.RefreshTokenTTLDays != 45 || cfg.Auth.BootstrapAdminEmail != "admin@example.com" {
		t.Fatalf("auth overrides not applied: %+v", cfg.Auth)
	}
	if len(cfg.Server.AllowedOrigins) != 2 || cfg.Server.AllowedOrigins[1] != "https://ops.example.com" {
		t.Fatalf("CORS origins not applied: %+v", cfg.Server.AllowedOrigins)
	}
	if cfg.ObjectStorage.Driver != "local" || cfg.ObjectStorage.Root != "/srv/tmk/objects" || cfg.ObjectStorage.MaxAudioBytes != 123456 || cfg.ObjectStorage.TotalQuotaBytes != 987654 {
		t.Fatalf("object storage overrides not applied: %+v", cfg.ObjectStorage)
	}
	if cfg.Evaluation.Workers != 3 || cfg.Evaluation.ItemTimeoutSeconds != 120 {
		t.Fatalf("evaluation overrides not applied: %+v", cfg.Evaluation)
	}
}
