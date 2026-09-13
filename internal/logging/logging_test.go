package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lokeshgoel177/loki/internal/logging"
)

// TestParseLevel verifies mapping of strings to standard slog.Level values.
func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown_level", slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if lvl := logging.ParseLevel(tc.input); lvl != tc.expected {
				t.Errorf("expected level %v for input %q, got %v", tc.expected, tc.input, lvl)
			}
		})
	}
}

// TestNew_TextHandler verifies text log output and level filtering.
func TestNew_TextHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{
		Level:  "info",
		Format: logging.FormatText,
		Output: &buf,
	})

	// Debug should be filtered out when Level is "info"
	logger.Debug("should be ignored")
	if buf.Len() > 0 {
		t.Fatalf("expected debug log to be filtered, got %q", buf.String())
	}

	// Info should be logged
	logger.Info("hello text logger", slog.String("subsystem", "ipc"))
	output := buf.String()

	if !strings.Contains(output, "level=INFO") {
		t.Errorf("expected level=INFO in output, got %q", output)
	}
	if !strings.Contains(output, "hello text logger") {
		t.Errorf("expected message in output, got %q", output)
	}
	if !strings.Contains(output, "subsystem=ipc") {
		t.Errorf("expected attribute in output, got %q", output)
	}
}

// TestNew_JSONHandler verifies valid JSON output and structure.
func TestNew_JSONHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{
		Level:  "debug",
		Format: logging.FormatJSON,
		Output: &buf,
	})

	logger.Debug("session created", slog.String("session_id", "sess-123"), slog.Int("turn", 1))

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("expected valid JSON output, got error: %v, raw: %q", err, buf.String())
	}

	if record["level"] != "DEBUG" {
		t.Errorf("expected level DEBUG, got %v", record["level"])
	}
	if record["msg"] != "session created" {
		t.Errorf("expected msg 'session created', got %v", record["msg"])
	}
	if record["session_id"] != "sess-123" {
		t.Errorf("expected session_id 'sess-123', got %v", record["session_id"])
	}
	if record["turn"] != float64(1) {
		t.Errorf("expected turn 1, got %v", record["turn"])
	}
}

// TestSetupGlobal verifies registering global slog default.
func TestSetupGlobal(t *testing.T) {
	var buf bytes.Buffer
	_ = logging.SetupGlobal(logging.Options{
		Level:  "info",
		Format: logging.FormatText,
		Output: &buf,
	})

	slog.Info("global message", slog.String("source", "test"))
	output := buf.String()

	if !strings.Contains(output, "global message") || !strings.Contains(output, "source=test") {
		t.Errorf("expected global logger to write to buffer, got %q", output)
	}
}

// TestNewFileLogger verifies file creation, write permissions, and disk appending.
func TestNewFileLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "sub", "agentd.log")

	logger, file, err := logging.NewFileLogger(logPath, "debug", logging.FormatJSON)
	if err != nil {
		t.Fatalf("failed to create file logger: %v", err)
	}
	defer file.Close()

	logger.Debug("disk write test", slog.String("status", "ok"))

	// Ensure bytes flushed to disk
	_ = file.Sync()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read written log file: %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to unmarshal JSON from file: %v, raw: %s", err, string(data))
	}

	if entry["msg"] != "disk write test" {
		t.Errorf("expected msg 'disk write test', got %v", entry["msg"])
	}
}
