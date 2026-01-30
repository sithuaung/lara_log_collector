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
	"time"

	"github.com/sithuaung/lara_log_collector/buffer"
	"github.com/sithuaung/lara_log_collector/config"
	"github.com/sithuaung/lara_log_collector/sender"
	"github.com/sithuaung/lara_log_collector/suppressor"
	"github.com/sithuaung/lara_log_collector/watcher"
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
	log.Printf("  Buffer size: %d", cfg.Buffer.Size)
	log.Printf("  Batch size: %d", cfg.Lark.BatchSize)

	var sup *suppressor.Suppressor
	if len(cfg.Suppress.Patterns) > 0 {
		var err error
		sup, err = suppressor.New(cfg.Suppress)
		if err != nil {
			log.Fatalf("Failed to init suppressor: %v", err)
		}
		log.Printf("  Suppressed patterns: %d (match=%s, case_insensitive=%t)", len(cfg.Suppress.Patterns), cfg.Suppress.Match, cfg.Suppress.CaseInsensitive)
		log.Printf("  Suppress report time: %s %s", cfg.Suppress.DailyReportTime, cfg.Suppress.Timezone)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create buffer
	buf := buffer.NewBuffer(cfg.Buffer.Size, cfg.Buffer.DropOldest)

	// Create and start sender (interface makes it easy to swap implementations)
	logSender := sender.NewLarkSender(cfg.Lark, buf, cfg.AppName)
	go logSender.Start(ctx)

	if sup != nil {
		go runSuppressedSummaryScheduler(ctx, logSender, sup, cfg.Suppress.DailyReportTime)
	}

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
		logWatcher := watcher.NewWatcherWithApp(cfg.Watcher, logDir, appName, buf, cfg.MinLogLevel, sup)
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

func runSuppressedSummaryScheduler(ctx context.Context, logSender *sender.LarkSender, sup *suppressor.Suppressor, reportTime string) {
	hour, minute, err := parseHourMinute(reportTime)
	if err != nil {
		log.Printf("Invalid suppress daily_report_time %q: %v (scheduler disabled)", reportTime, err)
		return
	}

	loc := sup.Location()
	for {
		now := time.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
		if !now.Before(next) {
			next = next.AddDate(0, 0, 1)
		}

		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		date, byApp := sup.Snapshot()
		reportAt := time.Now().In(loc)
		for app, counts := range byApp {
			if len(counts) == 0 {
				continue
			}
			if err := logSender.SendSuppressedSummary(app, date, reportAt, counts); err != nil {
				log.Printf("Failed to send suppressed summary (app=%q): %v", app, err)
			}
		}
	}
}

func parseHourMinute(value string) (int, int, error) {
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, err
	}
	return t.Hour(), t.Minute(), nil
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
