package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lokeshgoel177/loki/internal/config"
	"github.com/lokeshgoel177/loki/internal/daemon"
	"github.com/lokeshgoel177/loki/internal/logging"
)

func main() {
	configPath := flag.String("config", "", "Path to loki configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentd: failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Configure structured file logging to ~/.loki/agentd.log
	logFile := filepath.Join(cfg.LokiDir, "agentd.log")
	logger, fileHandle, err := logging.NewFileLogger(logFile, cfg.LogLevel, logging.FormatJSON)
	if err != nil {
		// Fallback to stderr if log file cannot be created
		logger = logging.New(logging.Options{
			Level:  cfg.LogLevel,
			Format: logging.FormatText,
			Output: os.Stderr,
		})
	} else {
		defer fileHandle.Close()
	}
	slog.SetDefault(logger)

	slog.Info("initializing agentd", "loki_dir", cfg.LokiDir, "socket", cfg.SocketPath)

	srv := daemon.NewServer(cfg, nil, nil)
	if err := srv.Start(); err != nil {
		slog.Error("failed to start agentd", "error", err)
		os.Exit(1)
	}

	// Listen for termination signals (SIGINT, SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	slog.Info("received shutdown signal", "signal", sig.String())

	if err := srv.Stop(); err != nil {
		slog.Error("error during graceful shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("agentd shutdown complete")
}
