package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL   string
	AdminPassword string
	JWTSecret     string
	Port          string
	UploadDir     string
}

func Load() *Config {
	godotenv.Load()

	return &Config{
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/menu_grid?sslmode=disable"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "changeme"),
		JWTSecret:     getEnv("JWT_SECRET", "dev-secret-change-me"),
		Port:          getEnv("PORT", "8080"),
		UploadDir:     getEnv("UPLOAD_DIR", "./uploads"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
