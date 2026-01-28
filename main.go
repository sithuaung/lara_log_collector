package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"lara_log_collector/buffer"
	"lara_log_collector/config"
	"lara_log_collector/sender"
	"lara_log_collector/watcher"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate required configuration
	if cfg.Lark.WebhookURL == "" {
		log.Fatal("Lark webhook URL is required (set in config or LARK_WEBHOOK_URL env)")
	}

	logDirs := cfg.LogDirectories
	if len(logDirs) == 0 {
		logDirs = []string{cfg.LogDirectory}
	}

	log.Printf("Starting Laravel Log Collector")
	log.Printf("  App name (default): %s", cfg.AppName)
	if len(logDirs) == 1 {
		log.Printf("  Log directory: %s", logDirs[0])
	} else {
		log.Printf("  Log directories: %s", strings.Join(logDirs, ", "))
	}
	log.Printf("  Min log level: %s", cfg.MinLogLevel)
	log.Printf("  Message max length: %d", cfg.MessageMaxLength)
	log.Printf("  Buffer size: %d", cfg.Buffer.Size)
	log.Printf("  Batch size: %d", cfg.Lark.BatchSize)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create buffer
	buf := buffer.NewBuffer(cfg.Buffer.Size, cfg.Buffer.DropOldest)

	// Create and start sender
	larkSender := sender.NewLarkSender(cfg.Lark, buf, cfg.MessageMaxLength, cfg.AppName)
	go larkSender.Start(ctx)

	// Create and start watchers
	errChan := make(chan error, len(logDirs))
	for _, logDir := range logDirs {
		appName := cfg.AppName
		if len(cfg.LogDirectories) > 0 {
			appName = deriveAppNameFromLogDir(logDir)
		}
		if appName == "" {
			appName = deriveAppNameFromLogDir(logDir)
		}
		log.Printf("  Watching: %s (app=%s)", logDir, appName)
		logWatcher := watcher.NewWatcherWithApp(cfg.Watcher, logDir, appName, buf, cfg.MinLogLevel, cfg.MessageMaxLength)
		go func(w *watcher.Watcher) {
			errChan <- w.Start(ctx)
		}(logWatcher)
	}

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()

	case err := <-errChan:
		if err != nil && err != context.Canceled {
			log.Printf("Watcher error: %v", err)
		}
	}

	// Print final stats
	received, dropped, pending := buf.Stats()
	log.Printf("Final stats: received=%d, dropped=%d, pending=%d", received, dropped, pending)

	log.Println("Shutdown complete")
}

func deriveAppNameFromLogDir(logDir string) string {
	cleaned := filepath.Clean(logDir)
	suffix := filepath.Join("storage", "logs")
	if strings.HasSuffix(cleaned, suffix) {
		parent := filepath.Dir(filepath.Dir(cleaned))
		name := filepath.Base(parent)
		if name == "current" {
			name = filepath.Base(filepath.Dir(parent))
		}
		if name != "." && name != string(filepath.Separator) {
			return name
		}
	}
	return filepath.Base(cleaned)
}
