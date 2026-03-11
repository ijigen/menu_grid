package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"menu_grid/internal/encrypt"
	"menu_grid/internal/model"
	"menu_grid/internal/storage"

	"github.com/go-chi/chi/v5"
)

type APIHandler struct {
	DB      *sql.DB
	Storage *storage.ImageStorage
	Enc     *encrypt.Encryptor
}

func NewAPIHandler(db *sql.DB, store *storage.ImageStorage, enc *encrypt.Encryptor) *APIHandler {
	return &APIHandler{DB: db, Storage: store, Enc: enc}
}

// ListPublishedWorks handles GET /api/works
func (h *APIHandler) ListPublishedWorks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, title, price, content, sort_order, published, created_at, updated_at
		FROM works WHERE published = true ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	works := []model.Work{}
	for rows.Next() {
		var w model.Work
		if err := rows.Scan(&w.ID, &w.Title, &w.Price, &w.Content, &w.SortOrder, &w.Published, &w.CreatedAt, &w.UpdatedAt); err != nil {
			continue
		}
		works = append(works, w)
	}

	for i := range works {
		works[i].Images, _ = h.loadImages(works[i].ID)
	}

	jsonResponse(w, works)
}

// GetWork handles GET /api/works/{id}
func (h *APIHandler) GetWork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var work model.Work
	err := h.DB.QueryRow(`
		SELECT id, title, price, content, sort_order, published, created_at, updated_at
		FROM works WHERE id = $1 AND published = true
	`, id).Scan(&work.ID, &work.Title, &work.Price, &work.Content, &work.SortOrder, &work.Published, &work.CreatedAt, &work.UpdatedAt)

	if err == sql.ErrNoRows {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	work.Images, _ = h.loadImages(work.ID)
	jsonResponse(w, work)
}

// ServePreviewImage handles GET /api/images/preview/{filename}
// Preview images are low-quality plaintext (no encryption).
func (h *APIHandler) ServePreviewImage(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if err := h.Storage.ServeImage(w, "preview", filename); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// ServeThumbImage handles GET /api/images/thumb/{filename}
// Encrypted with AES-256-GCM. Frontend must decrypt with derived key.
func (h *APIHandler) ServeThumbImage(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	h.serveEncrypted(w, "thumb", filename)
}

// ServeFullImage handles GET /api/images/full/{filename}
// Encrypted with AES-256-GCM. Frontend must decrypt with derived key.
func (h *APIHandler) ServeFullImage(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	h.serveEncrypted(w, "full", filename)
}

func (h *APIHandler) serveEncrypted(w http.ResponseWriter, imageType, filename string) {
	data, err := h.Storage.ReadFile(imageType, filename)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	encrypted, err := h.Enc.Encrypt(data)
	if err != nil {
		http.Error(w, "encryption error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(encrypted)
}

func (h *APIHandler) loadImages(workID int) ([]model.WorkImage, error) {
	rows, err := h.DB.Query(`
		SELECT id, work_id, filename, sort_order, is_cover, created_at
		FROM work_images WHERE work_id = $1 ORDER BY sort_order ASC, id ASC
	`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []model.WorkImage
	for rows.Next() {
		var img model.WorkImage
		if err := rows.Scan(&img.ID, &img.WorkID, &img.Filename, &img.SortOrder, &img.IsCover, &img.CreatedAt); err != nil {
			continue
		}
		images = append(images, img)
	}
	return images, nil
}

// JSON helpers
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
