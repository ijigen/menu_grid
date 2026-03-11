package model

import "time"

type Work struct {
	ID        int         `json:"id"`
	Title     string      `json:"title"`
	Price     string      `json:"price"`
	Content   string      `json:"content"`
	SortOrder int         `json:"sort_order"`
	Published bool        `json:"published"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Images    []WorkImage `json:"images,omitempty"`
}

// WorkImage represents one image with auto-generated variants.
// The same filename exists in uploads/preview/, uploads/thumb/, uploads/full/.
type WorkImage struct {
	ID        int       `json:"id"`
	WorkID    int       `json:"work_id"`
	Filename  string    `json:"filename"`
	SortOrder int       `json:"sort_order"`
	IsCover   bool      `json:"is_cover"`
	CreatedAt time.Time `json:"created_at"`
}

type SiteSetting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Phase 3 models
type FallbackDomain struct {
	ID        int       `json:"id"`
	Domain    string    `json:"domain"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type ClientDomainAssignment struct {
	ID         int       `json:"id"`
	ClientID   string    `json:"client_id"`
	DomainID   int       `json:"domain_id"`
	AssignedAt time.Time `json:"assigned_at"`
	IsValid    bool      `json:"is_valid"`
}
