package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ExportRecord struct {
	SourceText     string `json:"source_text"`
	TranslatedText string `json:"translated_text"`
	Sequence       int    `json:"sequence"`
}

type ExportService struct{}

func NewExportService() *ExportService {
	return &ExportService{}
}

func (s *ExportService) ExportTXT(title string, records []ExportRecord) (string, error) {
	if len(records) == 0 {
		return "", fmt.Errorf("no records to export")
	}
	var b strings.Builder
	for i, r := range records {
		seq := r.Sequence
		if seq == 0 {
			seq = i + 1
		}
		fmt.Fprintf(&b, "[%d]\n原文: %s\n译文: %s\n\n", seq, r.SourceText, r.TranslatedText)
	}
	return writeExportFile(title, "txt", b.String())
}

func (s *ExportService) ExportSRT(title string, records []ExportRecord) (string, error) {
	if len(records) == 0 {
		return "", fmt.Errorf("no records to export")
	}
	var b strings.Builder
	for i, r := range records {
		start := time.Duration(i*3) * time.Second
		end := start + 3*time.Second
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n%s\n\n",
			i+1,
			formatSRTTime(start),
			formatSRTTime(end),
			r.SourceText,
			r.TranslatedText,
		)
	}
	return writeExportFile(title, "srt", b.String())
}

func writeExportFile(title, ext, content string) (string, error) {
	dir, err := downloadsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := safeFileName(title)
	if name == "" {
		name = "tmk-export"
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.%s", name, time.Now().Format("20060102-150405"), ext))
	return path, os.WriteFile(path, []byte(content), 0o644)
}

func downloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads", "TMK"), nil
}

func safeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	re := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, ". ")
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

func formatSRTTime(d time.Duration) string {
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int(d / time.Second)
	d -= time.Duration(s) * time.Second
	ms := int(d / time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}
