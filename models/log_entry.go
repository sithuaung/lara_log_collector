package models

import "time"

// LogLevel represents the severity level of a log entry
type LogLevel string

const (
	LevelDebug     LogLevel = "DEBUG"
	LevelInfo      LogLevel = "INFO"
	LevelNotice    LogLevel = "NOTICE"
	LevelWarning   LogLevel = "WARNING"
	LevelError     LogLevel = "ERROR"
	LevelCritical  LogLevel = "CRITICAL"
	LevelAlert     LogLevel = "ALERT"
	LevelEmergency LogLevel = "EMERGENCY"
)

// LogEntry represents a parsed Laravel log entry
type LogEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	Environment string                 `json:"environment"`
	Level       LogLevel               `json:"level"`
	Message     string                 `json:"message"`
	Context     map[string]any `json:"context,omitempty"`
	StackTrace  string                 `json:"stack_trace,omitempty"`
	RawLine     string                 `json:"raw_line"`
}

// IsError returns true if the log level is ERROR or higher severity
func (e *LogEntry) IsError() bool {
	switch e.Level {
	case LevelError, LevelCritical, LevelAlert, LevelEmergency:
		return true
	default:
		return false
	}
}

// LevelColor returns the color code for Lark card based on log level
func (e *LogEntry) LevelColor() string {
	switch e.Level {
	case LevelEmergency, LevelAlert, LevelCritical:
		return "red"
	case LevelError:
		return "orange"
	case LevelWarning:
		return "yellow"
	case LevelInfo, LevelNotice:
		return "blue"
	default:
		return "grey"
	}
}

// Severity returns numeric severity (higher = more severe)
func (l LogLevel) Severity() int {
	switch l {
	case LevelDebug:
		return 0
	case LevelInfo:
		return 1
	case LevelNotice:
		return 2
	case LevelWarning:
		return 3
	case LevelError:
		return 4
	case LevelCritical:
		return 5
	case LevelAlert:
		return 6
	case LevelEmergency:
		return 7
	default:
		return 0
	}
}

// ParseLogLevel converts string to LogLevel
func ParseLogLevel(s string) LogLevel {
	switch s {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "NOTICE":
		return LevelNotice
	case "WARNING":
		return LevelWarning
	case "ERROR":
		return LevelError
	case "CRITICAL":
		return LevelCritical
	case "ALERT":
		return LevelAlert
	case "EMERGENCY":
		return LevelEmergency
	default:
		return LevelError
	}
}
