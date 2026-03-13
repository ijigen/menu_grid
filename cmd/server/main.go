package main

import (
	"fmt"
	"log"
	"net/http"

	"menu_grid/internal/config"
	"menu_grid/internal/database"
	"menu_grid/internal/encrypt"
	"menu_grid/internal/handler"
	"menu_grid/internal/middleware"
	"menu_grid/internal/storage"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	cfg := config.Load()

	// Database
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := database.RunMigrations(db, "./migrations"); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Storage & Encryption
	store := storage.NewImageStorage(cfg.UploadDir)
	enc := encrypt.NewEncryptor()

	// Handlers
	authHandler := handler.NewAuthHandler(db, cfg.JWTSecret, cfg.AdminPassword, enc)
	apiHandler := handler.NewAPIHandler(db, store, enc)
	adminHandler := handler.NewAdminHandler(db, store, enc)
	fallbackHandler := handler.NewFallbackHandler(db)

	// Router
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Static files
	fileServer := http.FileServer(http.Dir("./web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Service Worker must be served from root for scope
	r.Get("/sw.js", serveFileWithType("./web/static/js/sw.js", "application/javascript"))

	// Pages
	r.Get("/", serveFile("./web/templates/index.html"))
	r.Get("/admin", serveFile("./web/templates/admin.html"))

	// Public API
	r.Route("/api", func(r chi.Router) {
		r.Post("/verify-password", authHandler.VerifyPassword)
		r.Get("/works", apiHandler.ListPublishedWorks)
		r.Get("/works/{id}", apiHandler.GetWork)
		r.Get("/images/preview/{filename}", apiHandler.ServePreviewImage)
		r.Get("/images/thumb/{filename}", apiHandler.ServeThumbImage)
		r.Get("/images/full/{filename}", apiHandler.ServeFullImage)

		// Fallback domain (public)
		r.Get("/fallback/domains", fallbackHandler.GetDomains)
		r.Post("/fallback/report-failure", fallbackHandler.ReportFailure)
		r.Post("/fallback/request-replacement", fallbackHandler.RequestReplacement)

		// Admin
		r.Post("/admin/login", authHandler.AdminLogin)
		r.Route("/admin", func(r chi.Router) {
			r.Use(middleware.AdminAuth(cfg.JWTSecret))
			r.Get("/works", adminHandler.ListWorks)
			r.Post("/works", adminHandler.CreateWork)
			r.Put("/works/reorder", adminHandler.ReorderWorks)
			r.Put("/works/{id}", adminHandler.UpdateWork)
			r.Delete("/works/{id}", adminHandler.DeleteWork)
			r.Post("/works/{id}/images", adminHandler.UploadImage)
			r.Delete("/images/{id}", adminHandler.DeleteImage)
			r.Put("/images/{id}/set-cover", adminHandler.SetCover)
			r.Get("/settings", adminHandler.GetSettings)
			r.Put("/settings", adminHandler.UpdateSettings)
			r.Post("/regenerate-thumbnails", adminHandler.RegenerateThumbnails)
			// Plaintext image access for admin panel
			r.Get("/images/thumb/{filename}", adminHandler.ServeThumbPlain)
			r.Get("/images/full/{filename}", adminHandler.ServeFullPlain)
			// Fallback domain management
			r.Get("/fallback/domains", fallbackHandler.ListFallbackDomains)
			r.Post("/fallback/domains", fallbackHandler.AddFallbackDomain)
			r.Put("/fallback/domains/{id}/toggle", fallbackHandler.ToggleFallbackDomain)
			r.Delete("/fallback/domains/{id}", fallbackHandler.DeleteFallbackDomain)
		})
	})

	fmt.Printf("Server starting on :%s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}

func serveFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

func serveFileWithType(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, path)
	}
}
