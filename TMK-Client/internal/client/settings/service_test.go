package settings

import (
	"path/filepath"
	"testing"
)

func TestSettingsSaveLoadAndNormalize(t *testing.T) {
	service := &SettingsService{path: filepath.Join(t.TempDir(), "settings.json")}
	want := UserSettings{SelectedDevice: -2, SubtitleMounted: true, HistoryKeyword: "meeting"}
	if err := service.Save(want); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	got, err := service.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if got.SourceLang != "zh" || got.TargetLang != "en" || got.HistoryKeyword != want.HistoryKeyword {
		t.Fatalf("unexpected normalized settings: %+v", got)
	}
}

func TestSettingsLoadMissingFileReturnsDefaults(t *testing.T) {
	service := &SettingsService{path: filepath.Join(t.TempDir(), "missing", "settings.json")}
	got, err := service.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if got.SourceLang != "zh" || got.TargetLang != "en" || got.SelectedDevice != -2 || !got.SubtitleMounted {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}
