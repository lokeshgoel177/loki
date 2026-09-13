package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Format defines the serialization format for structured logs.
type Format string

const (
	// FormatText outputs human-friendly key-value pairs (ideal for CLI / stdout).
	FormatText Format = "text"
	// FormatJSON outputs structured newline-delimited JSON objects (ideal for daemon log files).
	FormatJSON Format = "json"
)

// Options configures the logger instantiation.
type Options struct {
	Level     string    // "debug", "info", "warn", "error" (case-insensitive)
	Format    Format    // FormatText or FormatJSON
	Output    io.Writer // Target writer (defaults to os.Stderr if nil)
	AddSource bool      // If true, includes source file and line number
}

func ParseLevel(lvl string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(lvl)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New builds and returns an *slog.Logger configured with the provided Options.
func New(opts Options) *slog.Logger {
	output := opts.Output
	if output == nil {
		output = os.Stderr
	}

	level := ParseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource,
	}

	var handler slog.Handler
	switch opts.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(output, handlerOpts)
	default:
		handler = slog.NewTextHandler(output, handlerOpts)
	}

	return slog.New(handler)
}

// SetupGlobal configures and registers the logger as the process-wide default (slog.Default()).
func SetupGlobal(opts Options) *slog.Logger {
	logger := New(opts)
	slog.SetDefault(logger)
	return logger
}

// NewFileLogger creates a logger that appends to a disk file (e.g. ~/.loki/agentd.log).
// It creates parent directories if missing and returns the logger along with the file handle.
func NewFileLogger(logFilePath string, level string, format Format) (*slog.Logger, *os.File, error) {
	dir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("logging: failed to create log directory %q: %w", dir, err)
	}

	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("logging: failed to open log file %q: %w", logFilePath, err)
	}

	logger := New(Options{
		Level:     level,
		Format:    format,
		Output:    f,
		AddSource: strings.ToLower(level) == "debug",
	})

	return logger, f, nil
}
