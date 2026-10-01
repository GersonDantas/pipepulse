package config

import (
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func encryptionKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://pipepulse:secret@localhost:5432/pipepulse")
	t.Setenv("DATA_ENCRYPTION_KEY", encryptionKey())
	t.Setenv("APP_BASE_URL", "https://api.pipepulse.app")
	t.Setenv("GITHUB_CLIENT_ID", "client-id")
	t.Setenv("GITHUB_CLIENT_SECRET", "client-secret")
	t.Setenv("MOBILE_OAUTH_REDIRECT_URI", "com.pipepulse.app://oauth")
	t.Setenv("FCM_CREDENTIALS_JSON_B64", base64.StdEncoding.EncodeToString([]byte(`{"type":"service_account"}`)))
}

func TestLoadRequiresFCMCredentials(t *testing.T) {
	for _, value := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("not-json"))} {
		t.Run(value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("FCM_CREDENTIALS_JSON_B64", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FCM_CREDENTIALS_JSON_B64") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadUsesEnvironmentAndDefaults(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("PORT", "8081")
	t.Setenv("ENVIRONMENT", "test")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("HTTP_READ_HEADER_TIMEOUT", "3s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "4s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "5s")
	t.Setenv("SHUTDOWN_TIMEOUT", "6s")
	t.Setenv("WORKER_POLL_INTERVAL", "250ms")
	t.Setenv("RETENTION_INTERVAL", "12h")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.Port != "8081" {
		t.Fatalf("Port = %q, want %q", configuration.Port, "8081")
	}
	if configuration.DatabaseURL != "postgres://pipepulse:secret@localhost:5432/pipepulse" {
		t.Fatalf("DatabaseURL = %q, want configured PostgreSQL URL", configuration.DatabaseURL)
	}
	if string(configuration.DataEncryptionKey) != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("DataEncryptionKey = %q, want decoded 32-byte key", configuration.DataEncryptionKey)
	}
	if configuration.AppBaseURL != "https://api.pipepulse.app" || configuration.GithubClientID != "client-id" || configuration.GithubClientSecret != "client-secret" || configuration.MobileOAuthRedirectURI != "com.pipepulse.app://oauth" {
		t.Fatalf("OAuth configuration = %#v", configuration)
	}
	if configuration.Environment != "test" {
		t.Fatalf("Environment = %q, want %q", configuration.Environment, "test")
	}
	if configuration.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel = %v, want %v", configuration.LogLevel, slog.LevelDebug)
	}
	if configuration.ReadHeaderTimeout != 3*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 3s", configuration.ReadHeaderTimeout)
	}
	if configuration.WriteTimeout != 4*time.Second {
		t.Fatalf("WriteTimeout = %s, want 4s", configuration.WriteTimeout)
	}
	if configuration.IdleTimeout != 5*time.Second {
		t.Fatalf("IdleTimeout = %s, want 5s", configuration.IdleTimeout)
	}
	if configuration.ShutdownTimeout != 6*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 6s", configuration.ShutdownTimeout)
	}
	if configuration.WorkerPollInterval != 250*time.Millisecond {
		t.Fatalf("WorkerPollInterval = %s, want 250ms", configuration.WorkerPollInterval)
	}
	if configuration.RetentionInterval != 12*time.Hour {
		t.Fatalf("RetentionInterval = %s, want 12h", configuration.RetentionInterval)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("HTTP_WRITE_TIMEOUT", "not-a-duration")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing DATABASE_URL error")
	}
}

func TestLoadRequiresValidDataEncryptionKey(t *testing.T) {
	setRequiredEnvironment(t)

	for _, value := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("too-short"))} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATA_ENCRYPTION_KEY", value)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want invalid DATA_ENCRYPTION_KEY error")
			}
		})
	}
}

func TestLoadRequiresOAuthConfiguration(t *testing.T) {
	for _, key := range []string{"APP_BASE_URL", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "MOBILE_OAUTH_REDIRECT_URI"} {
		t.Run(key, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv(key, "")
			if _, err := Load(); err == nil {
				t.Fatalf("Load() error = nil, want missing %s error", key)
			}
		})
	}
}
