package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"lara_log_collector/buffer"
	"lara_log_collector/config"
	"lara_log_collector/models"
)

// LarkSender sends log entries to Lark webhook
type LarkSender struct {
	cfg              config.LarkConfig
	client           *http.Client
	buffer           *buffer.Buffer
	batch            []*models.LogEntry
	batchMu          sync.Mutex
	flushTimer       *time.Timer
	messageMaxLength int
}

// NewLarkSender creates a new Lark webhook sender
func NewLarkSender(cfg config.LarkConfig, buf *buffer.Buffer, messageMaxLength int) *LarkSender {
	return &LarkSender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		buffer:           buf,
		batch:            make([]*models.LogEntry, 0, cfg.BatchSize),
		messageMaxLength: messageMaxLength,
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
	var lastErr error
	delay := s.cfg.RetryDelay

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		}

		if err := s.send(entries); err != nil {
			lastErr = err
			log.Printf("Lark send attempt %d failed: %v", attempt+1, err)
			continue
		}
		return
	}

	log.Printf("Failed to send %d log entries after %d retries: %v",
		len(entries), s.cfg.MaxRetries, lastErr)
}

// send sends a batch of entries to Lark
func (s *LarkSender) send(entries []*models.LogEntry) error {
	card := s.buildCard(entries)
	payload := s.buildPayload(card)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	log.Printf("Lark request: url=%s entries=%d payload_bytes=%d", redactWebhookURL(s.cfg.WebhookURL), len(entries), len(body))

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
		log.Printf("Lark response: status=%d body=%q", resp.StatusCode, truncateString(string(respBody), 1000))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) > 0 {
		log.Printf("Lark response: status=%d body=%q", resp.StatusCode, truncateString(string(respBody), 1000))
	} else {
		log.Printf("Lark response: status=%d", resp.StatusCode)
	}

	return nil
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
func (s *LarkSender) buildCard(entries []*models.LogEntry) map[string]any {
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
				"content": fmt.Sprintf("Laravel Logs (%d entries, %d groups)", len(entries), len(grouped)),
			},
		},
		"elements": elements,
	}
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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

	const messageLimit = 100
	byKey := make(map[string]*groupedEntry, len(entries))
	order := make([]string, 0, len(entries))

	for _, entry := range entries {
		message := truncateString(entry.Message, messageLimit)
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

func redactWebhookURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "***"
	}

	path := strings.TrimSuffix(parsed.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		parts[len(parts)-1] = "***"
	}
	parsed.Path = strings.Join(parts, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String()
}
