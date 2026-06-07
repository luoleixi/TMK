package model

import "time"

type Session struct {
	ID          string     `json:"id"`
	SourceLang  string     `json:"source_lang"`
	TargetLang  string     `json:"target_lang"`
	InputType   string     `json:"input_type"`
	Status      string     `json:"status"` // "active" | "ended"
	RecordCount int        `json:"record_count"`
	CreatedAt   time.Time  `json:"created_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

type Record struct {
	ID              int       `json:"id"`
	SessionID       string    `json:"session_id"`
	Sequence        int       `json:"sequence"`
	SourceText      string    `json:"source_text"`
	TranslatedText  string    `json:"translated_text"`
	Confidence      float64   `json:"confidence"`
	AudioDurationMs int       `json:"audio_duration_ms"`
	CreatedAt       time.Time `json:"created_at"`
}
