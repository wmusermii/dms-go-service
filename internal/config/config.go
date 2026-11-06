// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv         string
	HTTPAddr       string
	LogLevel       string
	TrustProxy     bool // honour X-Forwarded-For for audit IPs
	DatabaseURL    string
	MigrateOnStart bool
	AdminToken     string // empty disables the /admin/v1 endpoints

	MaxUploadBytes    int64
	AllowedExtensions []string

	Storage StorageConfig
	Scanner ScannerConfig

	ShutdownTimeout time.Duration
}

type StorageConfig struct {
	Driver    string // local | s3
	LocalPath string

	S3Endpoint     string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3Region       string
	S3UseSSL       bool
	S3CreateBucket bool
}

type ScannerConfig struct {
	Driver       string // disabled | noop (add more in connector/scanner)
	PollInterval time.Duration
	BatchSize    int
	Timeout      time.Duration
	MaxAttempts  int
	StaleAfter   time.Duration
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	l := &loader{}

	cfg := &Config{
		AppEnv:         l.str("APP_ENV", "development"),
		HTTPAddr:       l.str("HTTP_ADDR", ":8080"),
		LogLevel:       l.str("LOG_LEVEL", "info"),
		TrustProxy:     l.boolean("TRUST_PROXY", false),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		MigrateOnStart: l.boolean("MIGRATE_ON_START", true),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),

		MaxUploadBytes: int64(l.integer("MAX_UPLOAD_MB", 25)) * 1024 * 1024,
		AllowedExtensions: splitList(l.str("ALLOWED_EXTENSIONS",
			"pdf,png,jpg,jpeg,gif,webp,txt,csv,docx,xlsx,pptx")),

		Storage: StorageConfig{
			Driver:         l.str("STORAGE_DRIVER", "local"),
			LocalPath:      l.str("LOCAL_STORAGE_PATH", "./data/storage"),
			S3Endpoint:     os.Getenv("S3_ENDPOINT"),
			S3Bucket:       os.Getenv("S3_BUCKET"),
			S3AccessKey:    os.Getenv("S3_ACCESS_KEY"),
			S3SecretKey:    os.Getenv("S3_SECRET_KEY"),
			S3Region:       l.str("S3_REGION", "us-east-1"),
			S3UseSSL:       l.boolean("S3_USE_SSL", false),
			S3CreateBucket: l.boolean("S3_CREATE_BUCKET", false),
		},

		Scanner: ScannerConfig{
			Driver:       l.str("SCANNER_DRIVER", "disabled"),
			PollInterval: l.duration("SCAN_POLL_INTERVAL", 5*time.Second),
			BatchSize:    l.integer("SCAN_BATCH_SIZE", 10),
			Timeout:      l.duration("SCAN_TIMEOUT", 2*time.Minute),
			MaxAttempts:  l.integer("SCAN_MAX_ATTEMPTS", 3),
			StaleAfter:   l.duration("SCAN_STALE_AFTER", 10*time.Minute),
		},

		ShutdownTimeout: l.duration("SHUTDOWN_TIMEOUT", 20*time.Second),
	}

	if err := l.err(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.MaxUploadBytes <= 0 {
		errs = append(errs, errors.New("MAX_UPLOAD_MB must be > 0"))
	}
	if len(c.AllowedExtensions) == 0 {
		errs = append(errs, errors.New("ALLOWED_EXTENSIONS must not be empty"))
	}
	switch c.Storage.Driver {
	case "local":
		if c.Storage.LocalPath == "" {
			errs = append(errs, errors.New("LOCAL_STORAGE_PATH is required for STORAGE_DRIVER=local"))
		}
	case "s3":
		for k, v := range map[string]string{
			"S3_ENDPOINT": c.Storage.S3Endpoint, "S3_BUCKET": c.Storage.S3Bucket,
			"S3_ACCESS_KEY": c.Storage.S3AccessKey, "S3_SECRET_KEY": c.Storage.S3SecretKey,
		} {
			if v == "" {
				errs = append(errs, fmt.Errorf("%s is required for STORAGE_DRIVER=s3", k))
			}
		}
	default:
		errs = append(errs, fmt.Errorf("STORAGE_DRIVER %q is not supported (local|s3)", c.Storage.Driver))
	}
	if c.Scanner.BatchSize <= 0 {
		errs = append(errs, errors.New("SCAN_BATCH_SIZE must be > 0"))
	}
	if c.Scanner.MaxAttempts <= 0 {
		errs = append(errs, errors.New("SCAN_MAX_ATTEMPTS must be > 0"))
	}
	return errors.Join(errs...)
}

// ---- env helpers -----------------------------------------------------------

type loader struct{ errs []error }

func (l *loader) err() error { return errors.Join(l.errs...) }

func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (l *loader) integer(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s: %q is not an integer", key, v))
		return def
	}
	return n
}

func (l *loader) boolean(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s: %q is not a boolean", key, v))
		return def
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s: %q is not a duration (e.g. 30s, 5m)", key, v))
		return def
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p), "."))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
