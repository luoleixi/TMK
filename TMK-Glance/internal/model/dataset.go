package model

import "time"

const (
	ObjectKindAudio = "audio"
	ObjectKindText  = "text"

	ObjectStatusReady    = "ready"
	ObjectStatusDeleting = "deleting"

	DatasetStatusDraft    = "draft"
	DatasetStatusReady    = "ready"
	DatasetStatusArchived = "archived"
)

type StorageObject struct {
	ID           string     `json:"id"`
	OwnerUserID  string     `json:"owner_user_id"`
	Kind         string     `json:"kind"`
	OriginalName string     `json:"original_name"`
	StorageKey   string     `json:"-"`
	ContentType  string     `json:"content_type"`
	SizeBytes    int64      `json:"size_bytes"`
	SHA256       string     `json:"sha256"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type Dataset struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Language    string     `json:"language"`
	Status      string     `json:"status"`
	Revision    int        `json:"revision"`
	ItemCount   int        `json:"item_count"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ReadyAt     *time.Time `json:"ready_at,omitempty"`
}

type DatasetItem struct {
	ID                    string             `json:"id"`
	DatasetID             string             `json:"dataset_id"`
	Sequence              int                `json:"sequence"`
	AudioObjectID         string             `json:"audio_object_id"`
	AudioOriginalName     string             `json:"audio_original_name"`
	AudioSizeBytes        int64              `json:"audio_size_bytes"`
	ReferenceTextObjectID string             `json:"reference_text_object_id"`
	TextOriginalName      string             `json:"text_original_name"`
	TextSizeBytes         int64              `json:"text_size_bytes"`
	Notes                 string             `json:"notes"`
	ReferenceSegments     []ReferenceSegment `json:"reference_segments,omitempty"`
	CreatedBy             string             `json:"created_by"`
	CreatedAt             time.Time          `json:"created_at"`
}

// ReferenceSegment is an optional human annotation used for segmentation evaluation.
type ReferenceSegment struct {
	Text        string `json:"text"`
	BeginTimeMS int64  `json:"begin_time_ms,omitempty"`
	EndTimeMS   int64  `json:"end_time_ms,omitempty"`
}
