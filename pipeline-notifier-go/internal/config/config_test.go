package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoadUsesEnvironmentAndDefaults(t *testing.T) {
	t.Setenv("PORT", "8081")
	t.Setenv("ENVIRONMENT", "test")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("HTTP_READ_HEADER_TIMEOUT", "3s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "4s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "5s")
	t.Setenv("SHUTDOWN_TIMEOUT", "6s")
	t.Setenv("QUEUE_SIZE", "42")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.Port != "8081" {
		t.Fatalf("Port = %q, want %q", configuration.Port, "8081")
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
	if configuration.QueueSize != 42 {
		t.Fatalf("QueueSize = %d, want 42", configuration.QueueSize)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("HTTP_WRITE_TIMEOUT", "not-a-duration")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
