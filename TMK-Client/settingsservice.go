package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type UserSettings struct {
	SourceLang      string `json:"source_lang"`
	TargetLang      string `json:"target_lang"`
	SelectedDevice  int    `json:"selected_device"`
	SubtitleMounted bool   `json:"subtitle_mounted"`
	HistoryKeyword  string `json:"history_keyword"`
	HistoryDateFrom string `json:"history_date_from"`
	HistoryDateTo   string `json:"history_date_to"`
}

type SettingsService struct {
	mu   sync.Mutex
	path string
}

func NewSettingsService() *SettingsService {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return &SettingsService{path: filepath.Join(base, "TMK-Client", "settings.json")}
}

func defaultSettings() UserSettings {
	return UserSettings{
		SourceLang:      "zh",
		TargetLang:      "en",
		SelectedDevice:  deviceSystemAudio,
		SubtitleMounted: true,
	}
}

func (s *SettingsService) Load() (UserSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings := defaultSettings()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return defaultSettings(), err
	}
	normalizeSettings(&settings)
	return settings, nil
}

func (s *SettingsService) Save(settings UserSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalizeSettings(&settings)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func normalizeSettings(settings *UserSettings) {
	if settings.SourceLang == "" {
		settings.SourceLang = "zh"
	}
	if settings.TargetLang == "" {
		settings.TargetLang = "en"
	}
}
