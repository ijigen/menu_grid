package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"menu_grid/internal/encrypt"
	"menu_grid/internal/model"
	"menu_grid/internal/storage"

	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	DB      *sql.DB
	Storage *storage.ImageStorage
	Enc     *encrypt.Encryptor
}

func NewAdminHandler(db *sql.DB, store *storage.ImageStorage, enc *encrypt.Encryptor) *AdminHandler {
	return &AdminHandler{DB: db, Storage: store, Enc: enc}
}

// ListWorks handles GET /api/admin/works
func (h *AdminHandler) ListWorks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, title, price, content, sort_order, published, created_at, updated_at
		FROM works ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	works := []model.Work{}
	for rows.Next() {
		var work model.Work
		if err := rows.Scan(&work.ID, &work.Title, &work.Price, &work.Content, &work.SortOrder, &work.Published, &work.CreatedAt, &work.UpdatedAt); err != nil {
			continue
		}
		works = append(works, work)
	}

	for i := range works {
		works[i].Images, _ = h.loadImages(works[i].ID)
	}

	jsonResponse(w, works)
}

// CreateWork handles POST /api/admin/works
func (h *AdminHandler) CreateWork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		Price     string `json:"price"`
		Content   string `json:"content"`
		SortOrder int    `json:"sort_order"`
		Published bool   `json:"published"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	var id int
	err := h.DB.QueryRow(`
		INSERT INTO works (title, price, content, sort_order, published)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, req.Title, req.Price, req.Content, req.SortOrder, req.Published).Scan(&id)

	if err != nil {
		jsonError(w, "failed to create work", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]int{"id": id})
}

// UpdateWork handles PUT /api/admin/works/{id}
func (h *AdminHandler) UpdateWork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Title     string `json:"title"`
		Price     string `json:"price"`
		Content   string `json:"content"`
		SortOrder int    `json:"sort_order"`
		Published bool   `json:"published"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	result, err := h.DB.Exec(`
		UPDATE works SET title=$1, price=$2, content=$3, sort_order=$4, published=$5, updated_at=NOW()
		WHERE id=$6
	`, req.Title, req.Price, req.Content, req.SortOrder, req.Published, id)

	if err != nil {
		jsonError(w, "failed to update work", http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// DeleteWork handles DELETE /api/admin/works/{id}
func (h *AdminHandler) DeleteWork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idInt, _ := strconv.Atoi(id)

	// Delete image files
	images, _ := h.loadImages(idInt)
	for _, img := range images {
		h.Storage.DeleteAll(img.Filename)
	}

	result, err := h.DB.Exec("DELETE FROM works WHERE id = $1", id)
	if err != nil {
		jsonError(w, "failed to delete work", http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// ReorderWorks handles PUT /api/admin/works/reorder
func (h *AdminHandler) ReorderWorks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Orders []struct {
			ID        int `json:"id"`
			SortOrder int `json:"sort_order"`
		} `json:"orders"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	for _, o := range req.Orders {
		if _, err := tx.Exec("UPDATE works SET sort_order=$1, updated_at=NOW() WHERE id=$2", o.SortOrder, o.ID); err != nil {
			tx.Rollback()
			jsonError(w, "failed to reorder", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "failed to commit", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// UploadImage handles POST /api/admin/works/{id}/images
// Accepts one image file, auto-generates preview + thumb + full variants.
func (h *AdminHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	workID := chi.URLParam(r, "id")

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonError(w, "file too large", http.StatusBadRequest)
		return
	}

	isCover := r.FormValue("is_cover") == "true"

	file, _, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "no file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Save original + auto-generate thumb + preview
	filename, err := h.Storage.SaveWithVariants(file)
	if err != nil {
		jsonError(w, "failed to process image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// If setting as cover, unset existing covers for this work
	if isCover {
		h.DB.Exec("UPDATE work_images SET is_cover = false WHERE work_id = $1", workID)
	}

	var imgID int
	err = h.DB.QueryRow(`
		INSERT INTO work_images (work_id, filename, sort_order, is_cover)
		VALUES ($1, $2, COALESCE((SELECT MAX(sort_order)+1 FROM work_images WHERE work_id=$1), 0), $3)
		RETURNING id
	`, workID, filename, isCover).Scan(&imgID)

	if err != nil {
		h.Storage.DeleteAll(filename)
		jsonError(w, "failed to save image record", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"id": imgID, "filename": filename})
}

// DeleteImage handles DELETE /api/admin/images/{id}
func (h *AdminHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var img model.WorkImage
	err := h.DB.QueryRow("SELECT id, filename FROM work_images WHERE id = $1", id).
		Scan(&img.ID, &img.Filename)
	if err == sql.ErrNoRows {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	h.DB.Exec("DELETE FROM work_images WHERE id = $1", id)
	h.Storage.DeleteAll(img.Filename)

	jsonResponse(w, map[string]string{"status": "ok"})
}

// SetCover handles PUT /api/admin/images/{id}/set-cover
func (h *AdminHandler) SetCover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Find the image to get its work_id
	var workID int
	err := h.DB.QueryRow("SELECT work_id FROM work_images WHERE id = $1", id).Scan(&workID)
	if err == sql.ErrNoRows {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	// Unset all covers for this work, then set the selected one
	tx, err := h.DB.Begin()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	tx.Exec("UPDATE work_images SET is_cover = false WHERE work_id = $1", workID)
	tx.Exec("UPDATE work_images SET is_cover = true WHERE id = $1", id)
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		jsonError(w, "failed to update", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// GetSettings handles GET /api/admin/settings
func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query("SELECT key, value, updated_at FROM site_settings")
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	settings := map[string]string{}
	for rows.Next() {
		var s model.SiteSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			continue
		}
		settings[s.Key] = s.Value
	}

	jsonResponse(w, settings)
}

// UpdateSettings handles PUT /api/admin/settings
func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	for key, value := range req {
		_, err := h.DB.Exec(`
			INSERT INTO site_settings (key, value, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (key) DO UPDATE SET value=$2, updated_at=NOW()
		`, key, value)
		if err != nil {
			jsonError(w, "failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	// If unlock_password changed, regenerate salt and update encryptor
	if newPassword, ok := req["unlock_password"]; ok {
		salt, err := encrypt.GenerateSalt()
		if err == nil {
			h.DB.Exec(`INSERT INTO site_settings (key, value, updated_at)
				VALUES ('encryption_salt', $1, NOW())
				ON CONFLICT (key) DO UPDATE SET value=$1, updated_at=NOW()`, salt)
			h.Enc.SetCredentials(newPassword, salt)
		}
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// ServeThumbPlain serves thumb image as plaintext (admin only, behind JWT).
func (h *AdminHandler) ServeThumbPlain(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	w.Header().Set("Content-Type", "image/jpeg")
	if err := h.Storage.ServeImage(w, "thumb", filename); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// ServeFullPlain serves full image as plaintext (admin only, behind JWT).
func (h *AdminHandler) ServeFullPlain(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	w.Header().Set("Content-Type", "image/jpeg")
	if err := h.Storage.ServeImage(w, "full", filename); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *AdminHandler) loadImages(workID int) ([]model.WorkImage, error) {
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
