package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	sendMu     sync.Mutex
	lastSend   time.Time
	dedup      *deduper
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
		dedup:  newDeduper(cfg.DedupWindow),
	}
}

// Start begins processing log entries from the buffer
func (s *LarkSender) Start(ctx context.Context) {
	s.flushTimer = time.NewTimer(s.cfg.FlushInterval)
	defer s.flushTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.flushFinal()
			return

		case entry, ok := <-s.buffer.Channel():
			if !ok {
				s.flushFinal()
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

// flushFinal sends the batch plus every pending dedup summary
func (s *LarkSender) flushFinal() {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	s.flushBatchLocked(true)
}

// flushLocked sends the batch (must hold batchMu)
func (s *LarkSender) flushLocked() {
	s.flushBatchLocked(false)
}

func (s *LarkSender) flushBatchLocked(final bool) {
	entries := s.dedup.filter(s.batch, time.Now(), final)
	s.batch = make([]*models.LogEntry, 0, s.cfg.BatchSize)
	if len(entries) == 0 {
		return
	}

	// Send inline so slow requests and retries apply backpressure to the
	// bounded entry buffer instead of creating unlimited in-flight batches.
	s.sendWithRetry(entries)
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
	var errs []error
	for _, card := range s.buildCards(entries, appName) {
		if err := s.sendCardWithRetry(card, appName); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *LarkSender) sendCardWithRetry(card map[string]any, appName string) error {
	var lastErr error
	delay := s.cfg.RetryDelay

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		}

		if err := s.sendCard(card); err != nil {
			lastErr = err
			log.Printf("Lark send attempt %d failed (app=%q): %v", attempt+1, appName, err)
			continue
		}
		return nil
	}

	return lastErr
}

func (s *LarkSender) sendCard(card map[string]any) error {
	// Share pacing across batches, apps, retries, and daily summaries.
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if wait := time.Until(s.lastSend.Add(s.cfg.MinSendInterval)); wait > 0 {
		time.Sleep(wait)
	}
	s.lastSend = time.Now()
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

	var result struct {
		Code       int `json:"code"`
		StatusCode int `json:"StatusCode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return fmt.Errorf("decode Lark response: %w", err)
	}
	if result.Code != 0 || result.StatusCode != 0 {
		return fmt.Errorf("Lark rejected card: code=%d StatusCode=%d", result.Code, result.StatusCode)
	}

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

const (
	maxMessageBytes = 4000
	// Lark rejects webhook request bodies over 20KB.
	maxCardBytes = 18 * 1024
)

// buildCard creates a single Lark card message from log entries
func (s *LarkSender) buildCard(entries []*models.LogEntry, appName string) map[string]any {
	return s.buildCards(entries, appName)[0]
}

// buildCards groups entries and splits them across as many cards as needed to stay under Lark's size limit.
func (s *LarkSender) buildCards(entries []*models.LogEntry, appName string) []map[string]any {
	var cards []map[string]any
	var chunk []groupedEntry
	var elements []map[string]any
	size := 0
	for _, g := range groupEntries(entries) {
		g.Message = truncateMessage(g.Message, maxMessageBytes)
		els := s.groupElements(g)
		n := jsonSize(els)
		if len(chunk) > 0 && size+n > maxCardBytes {
			cards = append(cards, buildCardFromElements(chunk, elements, appName))
			chunk, elements, size = nil, nil, 0
		}
		if len(chunk) > 0 {
			elements = append(elements, map[string]any{"tag": "hr"})
		}
		chunk = append(chunk, g)
		elements = append(elements, els...)
		size += n
	}
	return append(cards, buildCardFromElements(chunk, elements, appName))
}

func (s *LarkSender) groupElements(g groupedEntry) []map[string]any {
	header := fmt.Sprintf("**[%s]** %s | %s (x%d)", g.Level, g.Environment, g.Timestamp.Format("2006-01-02 15:04:05"), g.Count)
	if g.Deduped {
		header += fmt.Sprintf(" — repeats held back by %s dedup", s.cfg.DedupWindow)
	}
	return []map[string]any{
		{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": header}},
		{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": g.Message}},
	}
}

func buildCardFromElements(grouped []groupedEntry, elements []map[string]any, appName string) map[string]any {
	headerColor := "blue"
	total := 0
	for _, g := range grouped {
		total += g.Count
		if g.Level.Severity() >= models.LevelError.Severity() {
			headerColor = "red"
		} else if g.Level == models.LevelWarning && headerColor != "red" {
			headerColor = "yellow"
		}
	}
	if elements == nil {
		elements = []map[string]any{}
	}

	return map[string]any{
		"header": map[string]any{
			"template": headerColor,
			"title": map[string]any{
				"tag":     "plain_text",
				"content": appName,
			},
			"text_tag_list": []map[string]any{
				textTag(fmt.Sprintf("%d entries", total), "orange"),
				textTag(fmt.Sprintf("%d groups", len(grouped)), "orange"),
			},
		},
		"elements": elements,
	}
}

func jsonSize(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

func truncateMessage(message string, max int) string {
	if len(message) <= max {
		return message
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… (truncated %d bytes)", message[:cut], len(message)-cut)
}

func textTag(content, color string) map[string]any {
	return map[string]any{
		"tag":   "text_tag",
		"text":  map[string]any{"tag": "plain_text", "content": content},
		"color": color,
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
	Deduped     bool
}

func groupEntries(entries []*models.LogEntry) []groupedEntry {
	if len(entries) == 0 {
		return nil
	}

	byKey := make(map[string]*groupedEntry, len(entries))
	order := make([]string, 0, len(entries))

	for _, entry := range entries {
		message := entry.Message
		key := fmt.Sprintf("%s|%s", entry.Level, messageKey(message))
		count := max(entry.Occurrences, 1)
		deduped := entry.Occurrences > 0

		if existing, ok := byKey[key]; ok {
			existing.Count += count
			existing.Deduped = existing.Deduped || deduped
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
			Count:       count,
			Deduped:     deduped,
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
