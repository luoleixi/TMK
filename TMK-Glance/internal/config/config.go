package config

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port           string   `yaml:"port"`
		AllowedOrigins []string `yaml:"allowed_origins"`
	} `yaml:"server"`
	Storage struct {
		Driver string `yaml:"driver"`
		DSN    string `yaml:"dsn"`
	} `yaml:"storage"`
	ObjectStorage struct {
		Driver            string `yaml:"driver"`
		Root              string `yaml:"root"`
		MaxAudioBytes     int64  `yaml:"max_audio_bytes"`
		MaxTextBytes      int64  `yaml:"max_text_bytes"`
		PerUserQuotaBytes int64  `yaml:"per_user_quota_bytes"`
		TotalQuotaBytes   int64  `yaml:"total_quota_bytes"`
		MinFreeBytes      int64  `yaml:"min_free_bytes"`
	} `yaml:"object_storage"`
	Evaluation struct {
		Workers               int `yaml:"workers"`
		PollIntervalMS        int `yaml:"poll_interval_ms"`
		ItemTimeoutSeconds    int `yaml:"item_timeout_seconds"`
		ChunkIntervalMS       int `yaml:"chunk_interval_ms"`
		LeaseSeconds          int `yaml:"lease_seconds"`
		HeartbeatSeconds      int `yaml:"heartbeat_seconds"`
		MaxAttempts           int `yaml:"max_attempts"`
		RetryBaseSeconds      int `yaml:"retry_base_seconds"`
		ReaperIntervalSeconds int `yaml:"reaper_interval_seconds"`
	} `yaml:"evaluation"`
	Governance struct {
		SessionRetentionDays    int `yaml:"session_retention_days"`
		EvaluationRetentionDays int `yaml:"evaluation_retention_days"`
		AuditRetentionDays      int `yaml:"audit_retention_days"`
		StaleDraftDays          int `yaml:"stale_draft_days"`
		StuckJobMinutes         int `yaml:"stuck_job_minutes"`
	} `yaml:"governance"`
	Auth struct {
		AccessTokenTTLMinutes  int    `yaml:"access_token_ttl_minutes"`
		RefreshTokenTTLDays    int    `yaml:"refresh_token_ttl_days"`
		BootstrapAdminEmail    string `yaml:"bootstrap_admin_email"`
		BootstrapAdminPassword string `yaml:"bootstrap_admin_password"`
	} `yaml:"auth"`
	AdminAPI struct {
		ServiceID     string `yaml:"service_id"`
		ServiceSecret string `yaml:"service_secret"`
	} `yaml:"admin_api"`
	ASR struct {
		Provider string `yaml:"provider"`
		Bailian  struct {
			APIKey                       string `yaml:"api_key"`
			MaxSentenceSilenceMS         int    `yaml:"max_sentence_silence_ms"`
			SemanticPunctuationEnabled   bool   `yaml:"semantic_punctuation_enabled"`
			MultiThresholdModeEnabled    bool   `yaml:"multi_threshold_mode_enabled"`
			PunctuationPredictionEnabled bool   `yaml:"punctuation_prediction_enabled"`
		} `yaml:"bailian"`
		Segmenter struct {
			Enabled           bool `yaml:"enabled"`
			MaxRunes          int  `yaml:"max_runes"`
			MaxDurationMS     int  `yaml:"max_duration_ms"`
			SoftCommitDelayMS int  `yaml:"soft_commit_delay_ms"`
		} `yaml:"segmenter"`
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
	cfg.Storage.Driver = "mysql"
	cfg.ObjectStorage.Driver = "local"
	cfg.ObjectStorage.Root = "./data/objects"
	cfg.ObjectStorage.MaxAudioBytes = 500 << 20
	cfg.ObjectStorage.MaxTextBytes = 10 << 20
	cfg.ObjectStorage.PerUserQuotaBytes = 5 << 30
	cfg.ObjectStorage.TotalQuotaBytes = 20 << 30
	cfg.ObjectStorage.MinFreeBytes = 2 << 30
	cfg.Evaluation.Workers = 1
	cfg.Evaluation.PollIntervalMS = 500
	cfg.Evaluation.ItemTimeoutSeconds = 600
	cfg.Evaluation.ChunkIntervalMS = 100
	cfg.Evaluation.LeaseSeconds = 60
	cfg.Evaluation.HeartbeatSeconds = 15
	cfg.Evaluation.MaxAttempts = 3
	cfg.Evaluation.RetryBaseSeconds = 5
	cfg.Evaluation.ReaperIntervalSeconds = 10
	cfg.Governance.SessionRetentionDays = 180
	cfg.Governance.EvaluationRetentionDays = 365
	cfg.Governance.AuditRetentionDays = 365
	cfg.Governance.StaleDraftDays = 30
	cfg.Governance.StuckJobMinutes = 30
	cfg.Auth.AccessTokenTTLMinutes = 15
	cfg.Auth.RefreshTokenTTLDays = 30
	cfg.AdminAPI.ServiceID = "tmk-admin-api"
	cfg.ASR.Provider = "mock"
	cfg.ASR.Bailian.MaxSentenceSilenceMS = 600
	cfg.ASR.Bailian.MultiThresholdModeEnabled = true
	cfg.ASR.Bailian.PunctuationPredictionEnabled = true
	cfg.ASR.Segmenter.MaxRunes = 40
	cfg.ASR.Segmenter.Enabled = true
	cfg.ASR.Segmenter.MaxDurationMS = 5000
	cfg.ASR.Segmenter.SoftCommitDelayMS = 300
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
	if v := os.Getenv("ASR_MAX_SENTENCE_SILENCE_MS"); v != "" {
		if value, parseErr := strconv.Atoi(v); parseErr == nil {
			cfg.ASR.Bailian.MaxSentenceSilenceMS = value
		}
	}
	if v := os.Getenv("TRANSLATOR_PROVIDER"); v != "" {
		cfg.Translator.Provider = v
	}
	cfg.Storage.Driver = "mysql"
	if v := os.Getenv("DB_DSN"); v != "" {
		cfg.Storage.DSN = v
	}
	if v := os.Getenv("OBJECT_STORAGE_DRIVER"); v != "" {
		cfg.ObjectStorage.Driver = v
	}
	if v := os.Getenv("OBJECT_STORAGE_ROOT"); v != "" {
		cfg.ObjectStorage.Root = v
	}
	applyInt64Env("OBJECT_STORAGE_MAX_AUDIO_BYTES", &cfg.ObjectStorage.MaxAudioBytes)
	applyInt64Env("OBJECT_STORAGE_MAX_TEXT_BYTES", &cfg.ObjectStorage.MaxTextBytes)
	applyInt64Env("OBJECT_STORAGE_PER_USER_QUOTA_BYTES", &cfg.ObjectStorage.PerUserQuotaBytes)
	applyInt64Env("OBJECT_STORAGE_TOTAL_QUOTA_BYTES", &cfg.ObjectStorage.TotalQuotaBytes)
	applyInt64Env("OBJECT_STORAGE_MIN_FREE_BYTES", &cfg.ObjectStorage.MinFreeBytes)
	applyIntEnv("EVALUATION_WORKERS", &cfg.Evaluation.Workers)
	applyIntEnv("EVALUATION_POLL_INTERVAL_MS", &cfg.Evaluation.PollIntervalMS)
	applyIntEnv("EVALUATION_ITEM_TIMEOUT_SECONDS", &cfg.Evaluation.ItemTimeoutSeconds)
	applyIntEnv("EVALUATION_CHUNK_INTERVAL_MS", &cfg.Evaluation.ChunkIntervalMS)
	applyIntEnv("EVALUATION_LEASE_SECONDS", &cfg.Evaluation.LeaseSeconds)
	applyIntEnv("EVALUATION_HEARTBEAT_SECONDS", &cfg.Evaluation.HeartbeatSeconds)
	applyIntEnv("EVALUATION_MAX_ATTEMPTS", &cfg.Evaluation.MaxAttempts)
	applyIntEnv("EVALUATION_RETRY_BASE_SECONDS", &cfg.Evaluation.RetryBaseSeconds)
	applyIntEnv("EVALUATION_REAPER_INTERVAL_SECONDS", &cfg.Evaluation.ReaperIntervalSeconds)
	applyIntEnv("GOVERNANCE_SESSION_RETENTION_DAYS", &cfg.Governance.SessionRetentionDays)
	applyIntEnv("GOVERNANCE_EVALUATION_RETENTION_DAYS", &cfg.Governance.EvaluationRetentionDays)
	applyIntEnv("GOVERNANCE_AUDIT_RETENTION_DAYS", &cfg.Governance.AuditRetentionDays)
	applyIntEnv("GOVERNANCE_STALE_DRAFT_DAYS", &cfg.Governance.StaleDraftDays)
	applyIntEnv("GOVERNANCE_STUCK_JOB_MINUTES", &cfg.Governance.StuckJobMinutes)
	if v := os.Getenv("AUTH_ACCESS_TOKEN_TTL_MINUTES"); v != "" {
		if value, parseErr := strconv.Atoi(v); parseErr == nil {
			cfg.Auth.AccessTokenTTLMinutes = value
		}
	}
	if v := os.Getenv("AUTH_REFRESH_TOKEN_TTL_DAYS"); v != "" {
		if value, parseErr := strconv.Atoi(v); parseErr == nil {
			cfg.Auth.RefreshTokenTTLDays = value
		}
	}
	if v := os.Getenv("ADMIN_API_SERVICE_ID"); v != "" {
		cfg.AdminAPI.ServiceID = v
	}
	if v := os.Getenv("ADMIN_API_SERVICE_SECRET"); v != "" {
		cfg.AdminAPI.ServiceSecret = v
	}
	if v := os.Getenv("AUTH_BOOTSTRAP_ADMIN_EMAIL"); v != "" {
		cfg.Auth.BootstrapAdminEmail = v
	}
	if v := os.Getenv("AUTH_BOOTSTRAP_ADMIN_PASSWORD"); v != "" {
		cfg.Auth.BootstrapAdminPassword = v
	}
	if v := os.Getenv("SERVER_PORT"); v != "" {
		cfg.Server.Port = v
	}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.Server.AllowedOrigins = nil
		for _, origin := range strings.Split(v, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				cfg.Server.AllowedOrigins = append(cfg.Server.AllowedOrigins, origin)
			}
		}
	}
	if cfg.Translator.Bailian.APIKey == "" {
		cfg.Translator.Bailian.APIKey = cfg.ASR.Bailian.APIKey
	}
	if cfg.Auth.AccessTokenTTLMinutes < 1 || cfg.Auth.AccessTokenTTLMinutes > 60 {
		cfg.Auth.AccessTokenTTLMinutes = 15
	}
	if cfg.Auth.RefreshTokenTTLDays < 1 || cfg.Auth.RefreshTokenTTLDays > 90 {
		cfg.Auth.RefreshTokenTTLDays = 30
	}
	if cfg.ObjectStorage.Driver == "" {
		cfg.ObjectStorage.Driver = "local"
	}
	if cfg.ObjectStorage.Root == "" {
		cfg.ObjectStorage.Root = "./data/objects"
	}
	if cfg.Evaluation.Workers < 1 || cfg.Evaluation.Workers > 8 {
		cfg.Evaluation.Workers = 1
	}
	if cfg.Evaluation.PollIntervalMS < 100 || cfg.Evaluation.PollIntervalMS > 60000 {
		cfg.Evaluation.PollIntervalMS = 500
	}
	if cfg.Evaluation.ItemTimeoutSeconds < 10 || cfg.Evaluation.ItemTimeoutSeconds > 3600 {
		cfg.Evaluation.ItemTimeoutSeconds = 600
	}
	if cfg.Evaluation.ChunkIntervalMS < 0 || cfg.Evaluation.ChunkIntervalMS > 1000 {
		cfg.Evaluation.ChunkIntervalMS = 100
	}
	if cfg.Evaluation.LeaseSeconds < 15 || cfg.Evaluation.LeaseSeconds > 3600 {
		cfg.Evaluation.LeaseSeconds = 60
	}
	if cfg.Evaluation.HeartbeatSeconds < 5 || cfg.Evaluation.HeartbeatSeconds >= cfg.Evaluation.LeaseSeconds {
		cfg.Evaluation.HeartbeatSeconds = cfg.Evaluation.LeaseSeconds / 4
	}
	if cfg.Evaluation.MaxAttempts < 1 || cfg.Evaluation.MaxAttempts > 10 {
		cfg.Evaluation.MaxAttempts = 3
	}
	if cfg.Evaluation.RetryBaseSeconds < 1 || cfg.Evaluation.RetryBaseSeconds > 300 {
		cfg.Evaluation.RetryBaseSeconds = 5
	}
	if cfg.Evaluation.ReaperIntervalSeconds < 1 || cfg.Evaluation.ReaperIntervalSeconds > 300 {
		cfg.Evaluation.ReaperIntervalSeconds = 10
	}
	cfg.Governance.SessionRetentionDays = boundedDays(cfg.Governance.SessionRetentionDays, 180)
	cfg.Governance.EvaluationRetentionDays = boundedDays(cfg.Governance.EvaluationRetentionDays, 365)
	cfg.Governance.AuditRetentionDays = boundedDays(cfg.Governance.AuditRetentionDays, 365)
	cfg.Governance.StaleDraftDays = boundedDays(cfg.Governance.StaleDraftDays, 30)
	if cfg.Governance.StuckJobMinutes < 5 || cfg.Governance.StuckJobMinutes > 1440 {
		cfg.Governance.StuckJobMinutes = 30
	}

	return cfg, nil
}

func boundedDays(value, fallback int) int {
	if value < 1 || value > 3650 {
		return fallback
	}
	return value
}

func applyInt64Env(name string, target *int64) {
	if value := os.Getenv(name); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			*target = parsed
		}
	}
}

func applyIntEnv(name string, target *int) {
	if value := os.Getenv(name); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			*target = parsed
		}
	}
}
