package handler

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"

	"menu_grid/internal/model"

	"github.com/go-chi/chi/v5"
)

const (
	DomainsPerClient = 4 // each client gets 4 fallback domains
)

type FallbackHandler struct {
	DB *sql.DB
}

func NewFallbackHandler(db *sql.DB) *FallbackHandler {
	return &FallbackHandler{DB: db}
}

// GetDomains handles GET /api/fallback/domains?client_id=xxx
// Returns the client's assigned fallback domain subset.
// If no assignment exists, creates one using consistent hashing.
func (h *FallbackHandler) GetDomains(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" || len(clientID) > 64 {
		jsonError(w, "invalid client_id", http.StatusBadRequest)
		return
	}

	// Check existing valid assignments
	assigned, err := h.getAssignedDomains(clientID)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	if len(assigned) >= DomainsPerClient {
		jsonResponse(w, map[string]interface{}{"domains": assigned})
		return
	}

	// Need more domains — assign from active pool
	needed := DomainsPerClient - len(assigned)
	newDomains, err := h.assignNewDomains(clientID, assigned, needed)
	if err != nil {
		// Return what we have
		jsonResponse(w, map[string]interface{}{"domains": assigned})
		return
	}

	all := append(assigned, newDomains...)
	jsonResponse(w, map[string]interface{}{"domains": all})
}

// ReportFailure handles POST /api/fallback/report-failure
// Client reports a fallback domain it couldn't reach.
func (h *FallbackHandler) ReportFailure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id"`
		Domain   string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Mark client's assignment as invalid
	h.DB.Exec(`
		UPDATE client_domain_assignments SET is_valid = false
		WHERE client_id = $1 AND domain_id = (SELECT id FROM fallback_domains WHERE domain = $2)
	`, req.ClientID, req.Domain)

	jsonResponse(w, map[string]string{"status": "ok"})
}

// RequestReplacement handles POST /api/fallback/request-replacement
// Client requests a new domain to replace a failed one.
func (h *FallbackHandler) RequestReplacement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID     string `json:"client_id"`
		FailedDomain string `json:"failed_domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Mark failed assignment as invalid
	h.DB.Exec(`
		UPDATE client_domain_assignments SET is_valid = false
		WHERE client_id = $1 AND domain_id = (SELECT id FROM fallback_domains WHERE domain = $2)
	`, req.ClientID, req.FailedDomain)

	// Get current valid assignments
	assigned, _ := h.getAssignedDomains(req.ClientID)

	// Assign one new domain
	newDomains, err := h.assignNewDomains(req.ClientID, assigned, 1)
	if err != nil || len(newDomains) == 0 {
		jsonResponse(w, map[string]interface{}{"domain": nil})
		return
	}

	jsonResponse(w, map[string]interface{}{"domain": newDomains[0]})
}

func (h *FallbackHandler) getAssignedDomains(clientID string) ([]string, error) {
	rows, err := h.DB.Query(`
		SELECT fd.domain FROM client_domain_assignments cda
		JOIN fallback_domains fd ON fd.id = cda.domain_id
		WHERE cda.client_id = $1 AND cda.is_valid = true AND fd.is_active = true
		ORDER BY cda.assigned_at ASC
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			domains = append(domains, d)
		}
	}
	return domains, nil
}

// assignNewDomains selects new domains for a client, avoiding already-assigned ones.
// Uses consistent hashing based on client_id for stable, distributed selection.
func (h *FallbackHandler) assignNewDomains(clientID string, alreadyAssigned []string, count int) ([]string, error) {
	// Get all active domains not already assigned to this client
	excludeSet := make(map[string]bool)
	for _, d := range alreadyAssigned {
		excludeSet[d] = true
	}

	rows, err := h.DB.Query(`
		SELECT id, domain FROM fallback_domains
		WHERE is_active = true
		AND id NOT IN (
			SELECT domain_id FROM client_domain_assignments
			WHERE client_id = $1 AND is_valid = true
		)
		ORDER BY id
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		ID     int
		Domain string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.ID, &c.Domain); err == nil {
			candidates = append(candidates, c)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Use client_id-seeded shuffle to get a stable, distributed selection.
	// Also factor in the "least assigned" count to spread domains across clients.
	seed := clientIDToSeed(clientID)
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	// Pick up to `count` candidates
	if count > len(candidates) {
		count = len(candidates)
	}
	selected := candidates[:count]

	// Insert assignments
	var newDomains []string
	for _, c := range selected {
		_, err := h.DB.Exec(`
			INSERT INTO client_domain_assignments (client_id, domain_id)
			VALUES ($1, $2)
			ON CONFLICT (client_id, domain_id) DO UPDATE SET is_valid = true, assigned_at = NOW()
		`, clientID, c.ID)
		if err == nil {
			newDomains = append(newDomains, c.Domain)
		}
	}

	return newDomains, nil
}

func clientIDToSeed(clientID string) int64 {
	h := sha256.Sum256([]byte(clientID))
	return int64(binary.BigEndian.Uint64(h[:8]))
}

// ===== Admin fallback domain management =====

// ListFallbackDomains handles GET /api/admin/fallback/domains
func (h *FallbackHandler) ListFallbackDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT fd.id, fd.domain, fd.is_active, fd.created_at,
			COUNT(DISTINCT cda.client_id) FILTER (WHERE cda.is_valid = true) as active_assignments
		FROM fallback_domains fd
		LEFT JOIN client_domain_assignments cda ON cda.domain_id = fd.id
		GROUP BY fd.id ORDER BY fd.id
	`)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type domainInfo struct {
		model.FallbackDomain
		ActiveAssignments int `json:"active_assignments"`
	}
	var domains []domainInfo
	for rows.Next() {
		var d domainInfo
		if err := rows.Scan(&d.ID, &d.Domain, &d.IsActive, &d.CreatedAt, &d.ActiveAssignments); err == nil {
			domains = append(domains, d)
		}
	}
	if domains == nil {
		domains = []domainInfo{}
	}

	jsonResponse(w, domains)
}

// AddFallbackDomain handles POST /api/admin/fallback/domains
func (h *FallbackHandler) AddFallbackDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	var id int
	err := h.DB.QueryRow(`INSERT INTO fallback_domains (domain) VALUES ($1) RETURNING id`, req.Domain).Scan(&id)
	if err != nil {
		jsonError(w, "failed to add domain (maybe duplicate)", http.StatusConflict)
		return
	}

	jsonResponse(w, map[string]int{"id": id})
}

// ToggleFallbackDomain handles PUT /api/admin/fallback/domains/{id}/toggle
func (h *FallbackHandler) ToggleFallbackDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := strconv.Atoi(id); err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	result, err := h.DB.Exec(`
		UPDATE fallback_domains SET is_active = NOT is_active WHERE id = $1
	`, id)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// DeleteFallbackDomain handles DELETE /api/admin/fallback/domains/{id}
func (h *FallbackHandler) DeleteFallbackDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := strconv.Atoi(id); err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	result, err := h.DB.Exec(`DELETE FROM fallback_domains WHERE id = $1`, id)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}
