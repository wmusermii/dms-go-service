package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort           string
	DatabaseURL       string
	StorageRoot       string
	APIKey            string
	JWTPrivateKeyPath string
	JWTPublicKeyPath  string
	JWTIssuer         string
	JWTAudience       string
	JWTExpiry         time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	jwtExpiry, err := time.ParseDuration(getEnv("JWT_EXPIRY", "1h"))
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_EXPIRY: %w", err)
	}

	cfg := &Config{
		AppPort:           getEnv("APP_PORT", "8080"),
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		StorageRoot:       getEnv("DMS_STORAGE_ROOT", "./storage/dms"),
		APIKey:            getEnv("DMS_API_KEY", ""),
		JWTPrivateKeyPath: getEnv("JWT_PRIVATE_KEY_PATH", ""),
		JWTPublicKeyPath:  getEnv("JWT_PUBLIC_KEY_PATH", ""),
		JWTIssuer:         getEnv("JWT_ISSUER", "dms-service"),
		JWTAudience:       getEnv("JWT_AUDIENCE", "dms-service"),
		JWTExpiry:         jwtExpiry,
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DMS_API_KEY is required")
	}

	if cfg.JWTPublicKeyPath == "" {
		return nil, fmt.Errorf("JWT_PUBLIC_KEY_PATH is required")
	}
	if cfg.JWTPrivateKeyPath == "" {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY_PATH is required")
	}

	if cfg.JWTIssuer == "" {
		return nil, fmt.Errorf("JWT_ISSUER is required")
	}

	if cfg.JWTAudience == "" {
		return nil, fmt.Errorf("JWT_AUDIENCE is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}

func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		log.Fatal(err)
	}

	return cfg
}
