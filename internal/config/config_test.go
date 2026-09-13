package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/config"
)

// TestNewDefaultConfig verifies default values and OS path conventions.
func TestNewDefaultConfig(t *testing.T) {
	cfg, err := config.NewDefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error creating default config: %v", err)
	}

	if runtime.GOOS == "windows" {
		if filepath.Base(cfg.ConfigDir) != "Loki" {
			t.Errorf("expected Windows config dir to end with 'Loki', got %q", cfg.ConfigDir)
		}
	} else {
		if filepath.Base(cfg.ConfigDir) != "loki" {
			t.Errorf("expected Unix config dir to end with 'loki', got %q", cfg.ConfigDir)
		}
	}

	if filepath.Base(cfg.LokiDir) != ".loki" {
		t.Errorf("expected LokiDir to end with '.loki', got %q", cfg.LokiDir)
	}

	if cfg.SocketPath == "" {
		t.Errorf("expected non-empty SocketPath")
	}

	if cfg.IdleTimeout.Duration() != 30*time.Minute {
		t.Errorf("expected 30m idle timeout, got %v", cfg.IdleTimeout)
	}

	if cfg.MaxConnections != 128 {
		t.Errorf("expected 128 max connections, got %d", cfg.MaxConnections)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("expected 'info' log level, got %q", cfg.LogLevel)
	}

	// Verify runtime path helpers
	if filepath.Base(cfg.PIDFile()) != "agentd.pid" {
		t.Errorf("expected PIDFile 'agentd.pid', got %q", cfg.PIDFile())
	}
	if filepath.Base(cfg.DatabasePath()) != "loki.db" {
		t.Errorf("expected DatabasePath 'loki.db', got %q", cfg.DatabasePath())
	}
}

// TestLoad_NonExistentFileReturnsDefaults ensures missing config file falls back gracefully.
func TestLoad_NonExistentFileReturnsDefaults(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "non-existent-config.json")
	cfg, err := config.Load(nonExistent)
	if err != nil {
		t.Fatalf("expected no error for missing config file, got %v", err)
	}

	if cfg.MaxConnections != 128 {
		t.Errorf("expected default max connections 128, got %d", cfg.MaxConnections)
	}
}

// TestLoad_ValidConfigFile verifies JSON unmarshaling and custom duration parsing.
func TestLoad_ValidConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")

	content := `{
            "socket_path": "\\\\.\\pipe\\custom-pipe",
            "idle_timeout": "15m",
            "max_connections": 64,
            "log_level": "debug"
        }`

	if err := os.WriteFile(configFile, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("failed to load valid config: %v", err)
	}

	if cfg.SocketPath != `\\.\pipe\custom-pipe` {
		t.Errorf("expected custom socket path, got %q", cfg.SocketPath)
	}

	if cfg.IdleTimeout.Duration() != 15*time.Minute {
		t.Errorf("expected 15m idle timeout, got %v", cfg.IdleTimeout.Duration())
	}

	if cfg.MaxConnections != 64 {
		t.Errorf("expected 64 max connections, got %d", cfg.MaxConnections)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("expected 'debug' log level, got %q", cfg.LogLevel)
	}

	// Verify unspecified fields retained their defaults
	if filepath.Base(cfg.LokiDir) != ".loki" {
		t.Errorf("expected default LokiDir, got %q", cfg.LokiDir)
	}
}

// TestLoad_MalformedJSON ensures invalid JSON returns an informative error.
func TestLoad_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "bad_config.json")

	if err := os.WriteFile(configFile, []byte(`{not-json}`), 0o600); err != nil {
		t.Fatalf("failed to write bad config: %v", err)
	}

	_, err := config.Load(configFile)
	if err == nil {
		t.Fatal("expected error parsing malformed JSON, got nil")
	}
}
