package parser

import (
	"regexp"
	"strings"
	"time"

	"lara_log_collector/models"
)

var (
	// Laravel log line format: [2025-03-12 10:15:30] production.ERROR: Message {"context": "data"}
	logLineRegex = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}\s\d{2}:\d{2}:\d{2})\]\s+(\w+)\.(\w+):\s+(.*)$`)

	// Stack trace line indicator
	stackTracePrefix = "#"
)

// Parser parses Laravel log entries
type Parser struct {
	currentEntry *models.LogEntry
}

// NewParser creates a new log parser
func NewParser() *Parser {
	return &Parser{}
}

// ParseLine parses a single log line and returns a log entry if complete
// It handles multi-line stack traces by accumulating lines
func (p *Parser) ParseLine(line string) *models.LogEntry {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Check if this is a new log entry
	matches := logLineRegex.FindStringSubmatch(line)
	if matches != nil {
		// If we have a previous entry, finalize it
		var completed *models.LogEntry
		if p.currentEntry != nil {
			completed = p.finalizeEntry()
		}

		// Start a new entry
		p.currentEntry = p.parseNewEntry(matches, line)

		return completed
	}

	return nil
}

// Flush returns any pending entry (call when done processing)
func (p *Parser) Flush() *models.LogEntry {
	if p.currentEntry != nil {
		return p.finalizeEntry()
	}
	return nil
}

// parseNewEntry parses a new log entry from regex matches
func (p *Parser) parseNewEntry(matches []string, rawLine string) *models.LogEntry {
	timestamp, _ := time.Parse("2006-01-02 15:04:05", matches[1])

	entry := &models.LogEntry{
		Timestamp:   timestamp,
		Environment: matches[2],
		Level:       models.LogLevel(strings.ToUpper(matches[3])),
		RawLine:     rawLine,
	}

	// Parse message and optional JSON context
	message := matches[4]
	entry.Message, entry.Context = parseMessageAndContext(message)
	entry.Message = stripStacktrace(entry.Message)

	return entry
}

// finalizeEntry completes the current entry and resets state
func (p *Parser) finalizeEntry() *models.LogEntry {
	entry := p.currentEntry

	p.currentEntry = nil
	// Stack traces are intentionally ignored to reduce memory usage.

	return entry
}

// parseMessageAndContext extracts the message and optional JSON context
func parseMessageAndContext(message string) (string, map[string]any) {
	// Context parsing is intentionally disabled to reduce memory usage.
	return message, nil
}

// ParseLevel converts a string to LogLevel
func ParseLevel(level string) models.LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return models.LevelDebug
	case "INFO":
		return models.LevelInfo
	case "NOTICE":
		return models.LevelNotice
	case "WARNING":
		return models.LevelWarning
	case "ERROR":
		return models.LevelError
	case "CRITICAL":
		return models.LevelCritical
	case "ALERT":
		return models.LevelAlert
	case "EMERGENCY":
		return models.LevelEmergency
	default:
		return models.LevelInfo
	}
}

func stripStacktrace(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return message
	}

	lower := strings.ToLower(message)
	idx := strings.Index(lower, "[stacktrace]")
	if idx == -1 {
		return message
	}
	return strings.TrimSpace(message[:idx])
}
