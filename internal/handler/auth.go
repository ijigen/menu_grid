package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"menu_grid/internal/encrypt"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB        *sql.DB
	JWTSecret string
	AdminHash string
	Enc       *encrypt.Encryptor
}

func NewAuthHandler(db *sql.DB, jwtSecret, adminPassword string, enc *encrypt.Encryptor) *AuthHandler {
	hash, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	h := &AuthHandler{
		DB:        db,
		JWTSecret: jwtSecret,
		AdminHash: string(hash),
		Enc:       enc,
	}
	// Initialize encryptor with current password + salt from DB
	h.initEncryptor()
	return h
}

func (h *AuthHandler) initEncryptor() {
	var password, salt string
	err := h.DB.QueryRow("SELECT value FROM site_settings WHERE key = 'unlock_password'").Scan(&password)
	if err != nil {
		return
	}
	err = h.DB.QueryRow("SELECT value FROM site_settings WHERE key = 'encryption_salt'").Scan(&salt)
	if err != nil {
		// No salt yet, generate one
		salt, _ = encrypt.GenerateSalt()
		h.DB.Exec(`INSERT INTO site_settings (key, value, updated_at) VALUES ('encryption_salt', $1, NOW())
			ON CONFLICT (key) DO UPDATE SET value=$1, updated_at=NOW()`, salt)
	}
	h.Enc.SetCredentials(password, salt)
}

// AdminLogin handles POST /api/admin/login
func (h *AuthHandler) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(h.AdminHash), []byte(req.Password)); err != nil {
		jsonError(w, "wrong password", http.StatusUnauthorized)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": "admin",
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	})
	tokenStr, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		jsonError(w, "failed to sign token", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"token": tokenStr})
}

// VerifyPassword handles POST /api/verify-password (front-end unlock password)
func (h *AuthHandler) VerifyPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	var storedPassword string
	err := h.DB.QueryRow("SELECT value FROM site_settings WHERE key = 'unlock_password'").Scan(&storedPassword)
	if err != nil {
		jsonError(w, "server error", http.StatusInternalServerError)
		return
	}

	if req.Password != storedPassword {
		jsonError(w, "wrong password", http.StatusUnauthorized)
		return
	}

	// Return encryption params so frontend can derive the same AES key
	var salt string
	h.DB.QueryRow("SELECT value FROM site_settings WHERE key = 'encryption_salt'").Scan(&salt)

	jsonResponse(w, map[string]interface{}{
		"success":    true,
		"salt":       salt,
		"iterations": encrypt.Iterations,
	})
}
