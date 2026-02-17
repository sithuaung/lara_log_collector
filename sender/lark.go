package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sithuaung/lara_log_collector/buffer"
	"github.com/sithuaung/lara_log_collector/config"
	"github.com/sithuaung/lara_log_collector/models"
)

// LarkSender sends log entries to Lark webhook
type LarkSender struct {
	cfg        config.LarkConfig
	client     *http.Client
	buffer     *buffer.Buffer
	batch      []*models.LogEntry
	batchMu    sync.Mutex
	flushTimer *time.Timer
}

// NewLarkSender creates a new Lark webhook sender
func NewLarkSender(cfg config.LarkConfig, buf *buffer.Buffer) *LarkSender {
	return &LarkSender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		buffer: buf,
		batch:  make([]*models.LogEntry, 0, cfg.BatchSize),
	}
}

// Start begins processing log entries from the buffer
func (s *LarkSender) Start(ctx context.Context) {
	s.flushTimer = time.NewTimer(s.cfg.FlushInterval)

	for {
		select {
		case <-ctx.Done():
			s.flush()
			return

		case entry, ok := <-s.buffer.Channel():
			if !ok {
				s.flush()
				return
			}
			s.addToBatch(entry)

		case <-s.flushTimer.C:
			s.flush()
			s.flushTimer.Reset(s.cfg.FlushInterval)
		}
	}
}

// addToBatch adds an entry to the current batch
func (s *LarkSender) addToBatch(entry *models.LogEntry) {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()

	s.batch = append(s.batch, entry)

	if len(s.batch) >= s.cfg.BatchSize {
		s.flushLocked()
	}
}

// flush sends the current batch to Lark
func (s *LarkSender) flush() {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	s.flushLocked()
}

// flushLocked sends the batch (must hold batchMu)
func (s *LarkSender) flushLocked() {
	if len(s.batch) == 0 {
		return
	}

	entries := s.batch
	s.batch = make([]*models.LogEntry, 0, s.cfg.BatchSize)

	go s.sendWithRetry(entries)
}

// sendWithRetry sends entries with exponential backoff retry
func (s *LarkSender) sendWithRetry(entries []*models.LogEntry) {
	byApp := groupEntriesByApp(entries)
	for appName, batch := range byApp {
		if len(batch) == 0 {
			continue
		}
		if err := s.sendWithRetryForApp(batch, appName); err != nil {
			log.Printf("Failed to send %d log entries for app %q after %d retries: %v",
				len(batch), appName, s.cfg.MaxRetries, err)
		}
	}
}

func (s *LarkSender) sendWithRetryForApp(entries []*models.LogEntry, appName string) error {
	var lastErr error
	delay := s.cfg.RetryDelay

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		}

		if err := s.send(entries, appName); err != nil {
			lastErr = err
			log.Printf("Lark send attempt %d failed (app=%q): %v", attempt+1, appName, err)
			continue
		}
		return nil
	}

	return lastErr
}

// send sends a batch of entries to Lark
func (s *LarkSender) send(entries []*models.LogEntry, appName string) error {
	card := s.buildCard(entries, appName)
	return s.sendCard(card)
}

func (s *LarkSender) sendCard(card map[string]any) error {
	payload := s.buildPayload(card)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	_, _ = io.ReadAll(resp.Body)

	return nil
}

// SendSuppressedSummary sends a daily summary of suppressed errors for an app.
func (s *LarkSender) SendSuppressedSummary(appName string, date string, reportTime time.Time, counts map[string]int64) error {
	if len(counts) == 0 {
		return nil
	}
	card := s.buildSuppressedSummaryCard(appName, date, reportTime, counts)
	return s.sendSummaryWithRetry(card)
}

func (s *LarkSender) sendSummaryWithRetry(card map[string]any) error {
	var lastErr error
	delay := s.cfg.RetryDelay

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2
		}
		if err := s.sendCard(card); err != nil {
			lastErr = err
			log.Printf("Lark summary send attempt %d failed: %v", attempt+1, err)
			continue
		}
		return nil
	}
	return lastErr
}

// buildPayload creates the Lark webhook payload
func (s *LarkSender) buildPayload(card map[string]any) map[string]any {
	payload := map[string]any{
		"msg_type": "interactive",
		"card":     card,
	}

	return payload
}

// buildCard creates a Lark card message from log entries
func (s *LarkSender) buildCard(entries []*models.LogEntry, appName string) map[string]any {
	// Determine header color based on highest severity
	headerColor := "blue"
	for _, entry := range entries {
		if entry.Level == models.LevelEmergency || entry.Level == models.LevelAlert || entry.Level == models.LevelCritical || entry.Level == models.LevelError {
			headerColor = "red"
			break
		} else if entry.Level == models.LevelWarning && headerColor != "red" {
			headerColor = "yellow"
		}
	}

	grouped := groupEntries(entries)
	elements := make([]map[string]any, 0, len(grouped)*2)

	for i, g := range grouped {
		// Add divider between entries
		if i > 0 {
			elements = append(elements, map[string]any{
				"tag": "hr",
			})
		}

		// Log level and timestamp
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**[%s]** %s | %s (x%d)", g.Level, g.Environment, g.Timestamp.Format("2006-01-02 15:04:05"), g.Count),
			},
		})

		// Message (trimmed to configured max length)
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": g.Message,
			},
		})
	}

	return map[string]any{
		"header": map[string]any{
			"template": headerColor,
			"title": map[string]any{
				"tag":     "plain_text",
				"content": fmt.Sprintf("%s (%d entries, %d groups)", appName, len(entries), len(grouped)),
			},
		},
		"elements": elements,
	}
}

func (s *LarkSender) buildSuppressedSummaryCard(appName string, date string, reportTime time.Time, counts map[string]int64) map[string]any {
	keys := make([]string, 0, len(counts))
	var total int64
	for k, v := range counts {
		keys = append(keys, k)
		total += v
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("- %s — x%d", k, counts[k]))
	}
	if len(lines) == 0 {
		lines = append(lines, "- (none)")
	}

	content := fmt.Sprintf("Suppressed errors for %s (up to %s)\nTotal: %d\n%s",
		date,
		reportTime.Format("15:04 MST"),
		total,
		strings.Join(lines, "\n"),
	)

	return map[string]any{
		"header": map[string]any{
			"template": "blue",
			"title": map[string]any{
				"tag":     "plain_text",
				"content": fmt.Sprintf("%s — suppressed summary", appName),
			},
		},
		"elements": []map[string]any{
			{
				"tag": "div",
				"text": map[string]any{
					"tag":     "lark_md",
					"content": content,
				},
			},
		},
	}
}

type groupedEntry struct {
	Level       models.LogLevel
	Environment string
	Timestamp   time.Time
	Message     string
	Count       int
}

func groupEntries(entries []*models.LogEntry) []groupedEntry {
	if len(entries) == 0 {
		return nil
	}

	byKey := make(map[string]*groupedEntry, len(entries))
	order := make([]string, 0, len(entries))

	for _, entry := range entries {
		message := entry.Message
		key := fmt.Sprintf("%s|%s", entry.Level, message)

		if existing, ok := byKey[key]; ok {
			existing.Count++
			if entry.Timestamp.After(existing.Timestamp) {
				existing.Timestamp = entry.Timestamp
				existing.Environment = entry.Environment
			}
			continue
		}

		byKey[key] = &groupedEntry{
			Level:       entry.Level,
			Environment: entry.Environment,
			Timestamp:   entry.Timestamp,
			Message:     message,
			Count:       1,
		}
		order = append(order, key)
	}

	grouped := make([]groupedEntry, 0, len(order))
	for _, key := range order {
		grouped = append(grouped, *byKey[key])
	}

	return grouped
}

func groupEntriesByApp(entries []*models.LogEntry) map[string][]*models.LogEntry {
	byApp := make(map[string][]*models.LogEntry)
	for _, entry := range entries {
		appName := entry.AppName
		if appName == "" {
			appName = "Unknown App"
		}
		byApp[appName] = append(byApp[appName], entry)
	}
	return byApp
}
