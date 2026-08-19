package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultPort               = "3000"
	defaultEnvironment        = "development"
	defaultLogLevel           = "info"
	defaultReadHeaderTimeout  = 5 * time.Second
	defaultWriteTimeout       = 15 * time.Second
	defaultIdleTimeout        = 60 * time.Second
	defaultShutdownTimeout    = 10 * time.Second
	defaultWorkerPollInterval = 5 * time.Second
	defaultRetentionInterval  = 24 * time.Hour
)

type Config struct {
	Port                   string
	DatabaseURL            string
	DataEncryptionKey      []byte
	AppBaseURL             string
	GithubClientID         string
	GithubClientSecret     string
	MobileOAuthRedirectURI string
	Environment            string
	LogLevel               slog.Level
	ReadHeaderTimeout      time.Duration
	WriteTimeout           time.Duration
	IdleTimeout            time.Duration
	ShutdownTimeout        time.Duration
	WorkerPollInterval     time.Duration
	RetentionInterval      time.Duration
}

func Load() (Config, error) {
	databaseURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	dataEncryptionKey, err := encryptionKeyFromEnv()
	if err != nil {
		return Config{}, err
	}
	appBaseURL, err := requiredAbsoluteURL("APP_BASE_URL", true)
	if err != nil {
		return Config{}, err
	}
	githubClientID, err := requiredEnv("GITHUB_CLIENT_ID")
	if err != nil {
		return Config{}, err
	}
	githubClientSecret, err := requiredEnv("GITHUB_CLIENT_SECRET")
	if err != nil {
		return Config{}, err
	}
	mobileOAuthRedirectURI, err := requiredAbsoluteURL("MOBILE_OAUTH_REDIRECT_URI", false)
	if err != nil {
		return Config{}, err
	}

	logLevel, err := parseLogLevel(envOrDefault("LOG_LEVEL", defaultLogLevel))
	if err != nil {
		return Config{}, err
	}

	readHeaderTimeout, err := durationFromEnv("HTTP_READ_HEADER_TIMEOUT", defaultReadHeaderTimeout)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := durationFromEnv("HTTP_WRITE_TIMEOUT", defaultWriteTimeout)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationFromEnv("HTTP_IDLE_TIMEOUT", defaultIdleTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationFromEnv("SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	workerPollInterval, err := durationFromEnv("WORKER_POLL_INTERVAL", defaultWorkerPollInterval)
	if err != nil {
		return Config{}, err
	}
	retentionInterval, err := durationFromEnv("RETENTION_INTERVAL", defaultRetentionInterval)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:                   envOrDefault("PORT", defaultPort),
		DatabaseURL:            databaseURL,
		DataEncryptionKey:      dataEncryptionKey,
		AppBaseURL:             strings.TrimRight(appBaseURL, "/"),
		GithubClientID:         githubClientID,
		GithubClientSecret:     githubClientSecret,
		MobileOAuthRedirectURI: mobileOAuthRedirectURI,
		Environment:            envOrDefault("ENVIRONMENT", defaultEnvironment),
		LogLevel:               logLevel,
		ReadHeaderTimeout:      readHeaderTimeout,
		WriteTimeout:           writeTimeout,
		IdleTimeout:            idleTimeout,
		ShutdownTimeout:        shutdownTimeout,
		WorkerPollInterval:     workerPollInterval,
		RetentionInterval:      retentionInterval,
	}, nil
}

func requiredAbsoluteURL(key string, webOnly bool) (string, error) {
	value, err := requiredEnv(key)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("%s must be an absolute URL", key)
	}
	if webOnly && (parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http")) {
		return "", fmt.Errorf("%s must be an HTTP(S) URL", key)
	}
	return value, nil
}

func encryptionKeyFromEnv() ([]byte, error) {
	value, err := requiredEnv("DATA_ENCRYPTION_KEY")
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("DATA_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	return key, nil
}

func requiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := envOrDefault(key, fallback.String())
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return duration, nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
}
