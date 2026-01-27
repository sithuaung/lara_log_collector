package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
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

	log.Printf("Starting Laravel Log Collector")
	log.Printf("  Log directory: %s", cfg.LogDirectory)
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
	larkSender := sender.NewLarkSender(cfg.Lark, buf, cfg.MessageMaxLength)
	go larkSender.Start(ctx)

	// Create and start watcher
	logWatcher := watcher.NewWatcher(cfg.Watcher, cfg.LogDirectory, buf, cfg.MinLogLevel, cfg.MessageMaxLength)

	// Start watcher in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- logWatcher.Start(ctx)
	}()

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
