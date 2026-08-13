package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Address           string
	PublicURL         string
	DatabaseURL       string
	DataKey           string
	ObjectRoot        string
	WebRoot           string
	Environment       string
	AdminUsername     string
	AdminPasswordHash string
	SecureCookies     bool
}

func FromEnvironment() (Config, error) {
	config := Config{
		Address:           valueOrDefault("SUPPORT_ADDRESS", ":8080"),
		PublicURL:         strings.TrimSuffix(valueOrDefault("SUPPORT_PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataKey:           strings.TrimSpace(os.Getenv("SUPPORT_DATA_KEY")),
		ObjectRoot:        valueOrDefault("SUPPORT_OBJECT_ROOT", "./data/private"),
		WebRoot:           valueOrDefault("SUPPORT_WEB_ROOT", "./frontend/dist"),
		Environment:       valueOrDefault("SUPPORT_ENVIRONMENT", "development"),
		AdminUsername:     strings.TrimSpace(os.Getenv("SUPPORT_ADMIN_USERNAME")),
		AdminPasswordHash: strings.TrimSpace(os.Getenv("SUPPORT_ADMIN_PASSWORD_HASH")),
	}
	parsed, err := url.Parse(config.PublicURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Config{}, errors.New("SUPPORT_PUBLIC_URL must be an absolute HTTP or HTTPS URL")
	}
	if config.Environment == "production" && parsed.Scheme != "https" {
		return Config{}, errors.New("production SUPPORT_PUBLIC_URL must use HTTPS")
	}
	config.SecureCookies = parsed.Scheme == "https"
	if config.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if config.DataKey == "" {
		return Config{}, errors.New("SUPPORT_DATA_KEY is required")
	}
	if len(config.AdminUsername) < 3 || len(config.AdminUsername) > 64 || strings.ContainsAny(config.AdminUsername, "\r\n\t ") {
		return Config{}, errors.New("SUPPORT_ADMIN_USERNAME must be 3 to 64 characters without whitespace")
	}
	if _, err := bcrypt.Cost([]byte(config.AdminPasswordHash)); err != nil {
		return Config{}, errors.New("SUPPORT_ADMIN_PASSWORD_HASH must be a valid bcrypt hash")
	}
	absoluteRoot, err := filepath.Abs(config.ObjectRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve SUPPORT_OBJECT_ROOT: %w", err)
	}
	config.ObjectRoot = absoluteRoot
	absoluteWebRoot, err := filepath.Abs(config.WebRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve SUPPORT_WEB_ROOT: %w", err)
	}
	config.WebRoot = absoluteWebRoot
	return config, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
