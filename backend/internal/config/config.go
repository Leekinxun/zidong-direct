package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                      string
	DataDir                   string
	MetaDir                   string
	SQLiteDir                 string
	AllowArbitrarySQLitePaths bool
	SecretKey                 string
	AuthEnabled               bool
	AdminUser                 string
	AdminPassword             string
	MaxQueryRows              int
	MaxUploadBytes            int64
	QueryTimeout              time.Duration
}

func Load() Config {
	dataDir := getEnv("DATA_DIR", "/data")
	sqliteDir := getEnv("SQLITE_DIR", filepath.Join(dataDir, "sqlite"))
	metaDir := filepath.Join(dataDir, "meta")

	adminPassword := os.Getenv("APP_ADMIN_PASSWORD")
	authDisabled := strings.EqualFold(os.Getenv("APP_AUTH_DISABLED"), "true")
	authEnabled := !authDisabled && adminPassword != ""

	return Config{
		Port:                      getEnv("PORT", "8080"),
		DataDir:                   dataDir,
		MetaDir:                   metaDir,
		SQLiteDir:                 sqliteDir,
		AllowArbitrarySQLitePaths: strings.EqualFold(os.Getenv("ALLOW_ARBITRARY_SQLITE_PATHS"), "true"),
		SecretKey:                 os.Getenv("APP_SECRET_KEY"),
		AuthEnabled:               authEnabled,
		AdminUser:                 getEnv("APP_ADMIN_USER", "admin"),
		AdminPassword:             adminPassword,
		MaxQueryRows:              getEnvInt("MAX_QUERY_ROWS", 1000),
		MaxUploadBytes:            getEnvInt64("MAX_UPLOAD_BYTES", 256*1024*1024),
		QueryTimeout:              time.Duration(getEnvInt("QUERY_TIMEOUT_SECONDS", 60)) * time.Second,
	}
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func getEnvInt64(key string, fallback int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
