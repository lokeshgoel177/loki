package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/lokeshgoel177/loki/internal/ipc"
)

type Duration time.Duration

// Duration converts the wrapper back to the standard time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// MarshalJSON formats the duration as a readable string like "30m0s".
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON parses both human strings ("30m") and numeric nanoseconds from JSON.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("config: invalid duration string %q: %w", value, err)
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("config: invalid duration type: %T", v)
	}
}

type Config struct {
	// ConfigDir is the directory containing user configurations (permissions.toml, mcp.json, config.json).
	ConfigDir string `json:"config_dir"`

	// LokiDir is the persistent runtime directory (~/.loki) housing the PID file, SQLite DB, and artifacts.
	LokiDir string `json:"loki_dir"`

	// SocketPath is the OS IPC address (Windows Named Pipe or Unix Domain Socket).
	SocketPath string `json:"socket_path"`

	// IdleTimeout defines how long inactive sessions remain in memory before heap eviction (default 30m).
	IdleTimeout Duration `json:"idle_timeout"`

	// MaxConnections limits the maximum concurrent IPC connections to agentd.
	MaxConnections int `json:"max_connections"`

	// LogLevel specifies the logging verbosity ("debug", "info", "warn", "error").
	LogLevel string `json:"log_level"`
}

// DefaultConfigDir returns the OS-specific default configuration directory:
// - Windows: %APPDATA%\Loki
// - Linux / macOS: $XDG_CONFIG_HOME/loki or ~/.config/loki
func DefaultConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Loki"), nil
		}
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "loki"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: failed to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "loki"), nil
}

// DefaultLokiDir returns the default runtime data directory: ~/.loki
func DefaultLokiDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: failed to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".loki"), nil
}

// NewDefaultConfig initializes a Config populated with canonical v2 defaults.
func NewDefaultConfig() (*Config, error) {
	configDir, err := DefaultConfigDir()
	if err != nil {
		return nil, err
	}

	lokiDir, err := DefaultLokiDir()
	if err != nil {
		return nil, err
	}

	socketPath, err := ipc.GetDefaultAddress()
	if err != nil {
		return nil, err
	}

	return &Config{
		ConfigDir:      configDir,
		LokiDir:        lokiDir,
		SocketPath:     socketPath,
		IdleTimeout:    Duration(30 * time.Minute),
		MaxConnections: 128,
		LogLevel:       "info",
	}, nil
}

// Load reads and parses a configuration file from the specified path.
// If path is empty (""), it looks for "config.json" inside DefaultConfigDir().
// If the file does not exist, it cleanly returns the default configuration without error.
func Load(path string) (*Config, error) {
	cfg, err := NewDefaultConfig()
	if err != nil {
		return nil, err
	}

	if path == "" {
		path = filepath.Join(cfg.ConfigDir, "config.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: failed to read config file %q: %w", path, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse config JSON in %q: %w", path, err)
	}

	return cfg, nil
}

// Helper methods computing canonical runtime file paths
func (c *Config) PIDFile() string       { return filepath.Join(c.LokiDir, "agentd.pid") }
func (c *Config) SpawnLockFile() string { return filepath.Join(c.LokiDir, "spawn.lock") }
func (c *Config) DatabasePath() string  { return filepath.Join(c.LokiDir, "loki.db") }
func (c *Config) ArtifactsDir() string  { return filepath.Join(c.LokiDir, "artifacts") }
