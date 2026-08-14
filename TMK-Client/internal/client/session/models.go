package session

type AuthUser struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	DisplayName        string `json:"display_name"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	MustChangePassword bool   `json:"must_change_password"`
}

type TranscriptMsg struct {
	Seq       int64  `json:"seq"`
	SegmentID int64  `json:"segment_id"`
	Revision  int64  `json:"revision"`
	Text      string `json:"text"`
	IsFinal   bool   `json:"is_final"`
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
}

type TranslationMsg struct {
	Seq       int64  `json:"seq"`
	SegmentID int64  `json:"segment_id"`
	Revision  int64  `json:"revision"`
	Text      string `json:"text"`
	IsFinal   bool   `json:"is_final"`
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
}

type HistorySession struct {
	ID          string `json:"id"`
	SourceLang  string `json:"source_lang"`
	TargetLang  string `json:"target_lang"`
	Status      string `json:"status"`
	RecordCount int    `json:"record_count"`
	Brief       string `json:"brief,omitempty"`
	CreatedAt   string `json:"created_at"`
	EndedAt     string `json:"ended_at,omitempty"`
}

type HistoryRecord struct {
	ID              int     `json:"id"`
	SessionID       string  `json:"session_id"`
	Sequence        int     `json:"sequence"`
	SourceText      string  `json:"source_text"`
	TranslatedText  string  `json:"translated_text"`
	Confidence      float64 `json:"confidence"`
	AudioDurationMs int     `json:"audio_duration_ms"`
	CreatedAt       string  `json:"created_at"`
}

type HistoryDetail struct {
	SessionID       string          `json:"session_id"`
	SourceLang      string          `json:"source_lang"`
	TargetLang      string          `json:"target_lang"`
	DurationSeconds int             `json:"duration_seconds"`
	Summary         string          `json:"summary"`
	CreatedAt       string          `json:"created_at"`
	EndedAt         string          `json:"ended_at,omitempty"`
	Records         []HistoryRecord `json:"records"`
}
