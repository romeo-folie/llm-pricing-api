package models

import "time"

// Modality represents the input/output modality of an LLM model,
// matching the CHECK constraint on the models.modality column.
type Modality string

const (
	ModalityText       Modality = "text"
	ModalityMultimodal Modality = "multimodal"
	ModalityImage      Modality = "image"
	ModalityAudio      Modality = "audio"
	ModalityEmbedding  Modality = "embedding"
)

// Model mirrors the models table.
type Model struct {
	ID            int
	Provider      string
	Name          string
	Slug          string
	Modality      Modality
	ContextWindow *int // nullable in DB
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SourceType represents the method used to collect pricing data,
// matching the CHECK constraint on the sources.type column.
type SourceType string

const (
	SourceTypeAPI    SourceType = "api"
	SourceTypeGitHub SourceType = "github"
	SourceTypeScrape SourceType = "scrape"
)

// Source mirrors the sources table.
type Source struct {
	ID        int
	Name      string
	URL       string
	Type      SourceType
	CreatedAt time.Time
}
