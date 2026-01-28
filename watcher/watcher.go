package watcher

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"lara_log_collector/buffer"
	"lara_log_collector/config"
	"lara_log_collector/models"
	"lara_log_collector/parser"
)

// Watcher monitors Laravel log files and sends entries to the buffer
type Watcher struct {
	cfg           config.WatcherConfig
	logDir        string
	appName       string
	buffer        *buffer.Buffer
	parser        *parser.Parser
	currentFile   string
	offset        int64
	minLevel      models.LogLevel
}

// NewWatcher creates a new log file watcher
func NewWatcher(cfg config.WatcherConfig, logDir string, buf *buffer.Buffer, minLogLevel string) *Watcher {
	return &Watcher{
		cfg:           cfg,
		logDir:        logDir,
		appName:       "",
		buffer:        buf,
		parser:        parser.NewParser(),
		minLevel:      models.ParseLogLevel(minLogLevel),
	}
}

// NewWatcherWithApp creates a new log file watcher with an app name for tagging entries
func NewWatcherWithApp(cfg config.WatcherConfig, logDir string, appName string, buf *buffer.Buffer, minLogLevel string) *Watcher {
	w := NewWatcher(cfg, logDir, buf, minLogLevel)
	w.appName = appName
	return w
}

// Start begins watching for log files
func (w *Watcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Initial check
	if err := w.checkLogs(); err != nil {
		log.Printf("Initial log check failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			// Flush any pending entries
			if entry := w.parser.Flush(); entry != nil {
				w.buffer.Push(entry)
			}
			return ctx.Err()

		case <-ticker.C:
			if err := w.checkLogs(); err != nil {
				log.Printf("Log check failed: %v", err)
			}
		}
	}
}

// checkLogs checks for new log entries
func (w *Watcher) checkLogs() error {
	// Get current date's log file
	logFile := w.getCurrentLogFile()

	// Check if we need to switch files (new day)
	if logFile != w.currentFile {
		// Flush parser for previous file
		if entry := w.parser.Flush(); entry != nil {
			w.buffer.Push(entry)
		}
		w.currentFile = logFile
		w.offset = 0
		w.parser = parser.NewParser()
	}

	// Check if file exists
	info, err := os.Stat(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, that's OK
		}
		return fmt.Errorf("stat log file: %w", err)
	}

	// Check if file was truncated (log rotation)
	if info.Size() < w.offset {
		w.offset = 0
	}

	// Nothing new to read
	if info.Size() == w.offset {
		return nil
	}

	// Read new content
	return w.readNewLines(logFile)
}

// getCurrentLogFile returns the path to today's log file
func (w *Watcher) getCurrentLogFile() string {
	filename := "laravel-" + time.Now().Format("2006-01-02") + ".log"
	return filepath.Join(w.logDir, filename)
}

// readNewLines reads and processes new lines from the log file
func (w *Watcher) readNewLines(logFile string) error {
	f, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	// Seek to last read position
	if w.offset > 0 {
		if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
			return fmt.Errorf("seek: %w", err)
		}
	}

	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if entry := w.parser.ParseLine(line); entry != nil {
			// Filter by minimum log level
			if entry.Level.Severity() >= w.minLevel.Severity() {
				entry.AppName = w.appName
				w.buffer.Push(entry)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	// Update offset
	newOffset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("get offset: %w", err)
	}
	w.offset = newOffset

	return nil
}

// GetCurrentFile returns the current log file being watched
func (w *Watcher) GetCurrentFile() string {
	return w.currentFile
}

// ProcessEntry is a helper to manually push an entry (for testing)
func (w *Watcher) ProcessEntry(entry *models.LogEntry) {
	w.buffer.Push(entry)
}
