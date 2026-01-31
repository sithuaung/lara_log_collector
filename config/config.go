package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the log collector
type Config struct {
	Apps        []AppConfig    `yaml:"apps"`
	MinLogLevel string         `yaml:"min_log_level"`
	Lark        LarkConfig     `yaml:"lark"`
	Buffer      BufferConfig   `yaml:"buffer"`
	Watcher     WatcherConfig  `yaml:"watcher"`
	Suppress    SuppressConfig `yaml:"suppress"`
}

// AppConfig binds an app name to its Laravel log directory.
type AppConfig struct {
	Name         string `yaml:"name"`
	LogDirectory string `yaml:"log_directory"`
}

// LarkConfig holds Lark webhook configuration
type LarkConfig struct {
	WebhookURL    string        `yaml:"webhook_url"`
	BatchSize     int           `yaml:"batch_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	MaxRetries    int           `yaml:"max_retries"`
	RetryDelay    time.Duration `yaml:"retry_delay"`
}

// BufferConfig holds buffer configuration
type BufferConfig struct {
	Size       int  `yaml:"size"`
	DropOldest bool `yaml:"drop_oldest"`
}

// WatcherConfig holds file watcher configuration
type WatcherConfig struct {
	PollInterval  time.Duration `yaml:"poll_interval"`
	StateFilename string        `yaml:"state_filename"`
}

// SuppressConfig holds suppression and daily summary settings
type SuppressConfig struct {
	Patterns        []string `yaml:"patterns"`
	Match           string   `yaml:"match"` // "substring" or "regex"
	CaseInsensitive bool     `yaml:"case_insensitive"`
	DailyReportTime string   `yaml:"daily_report_time"` // "HH:MM"
	Timezone        string   `yaml:"timezone"`          // e.g. "Asia/Bangkok"
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Apps:        nil,
		MinLogLevel: "ERROR",
		Lark: LarkConfig{
			WebhookURL:    "",
			BatchSize:     10,
			FlushInterval: 5 * time.Second,
			MaxRetries:    3,
			RetryDelay:    1 * time.Second,
		},
		Buffer: BufferConfig{
			Size:       10000,
			DropOldest: true,
		},
		Watcher: WatcherConfig{
			PollInterval:  1 * time.Second,
			StateFilename: "",
		},
		Suppress: SuppressConfig{
			Patterns:        nil,
			Match:           "substring",
			CaseInsensitive: true,
			DailyReportTime: "17:00",
			Timezone:        "Asia/Bangkok",
		},
	}
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Override with environment variables if set
	cfg.applyEnvOverrides()

	return cfg, nil
}

// applyEnvOverrides overrides config values from environment variables
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("LARK_WEBHOOK_URL"); v != "" {
		c.Lark.WebhookURL = v
	}
	if v := os.Getenv("MIN_LOG_LEVEL"); v != "" {
		c.MinLogLevel = v
	}
}

// ResolveApps returns explicit app configs.
func (c *Config) ResolveApps() []AppConfig {
	if c == nil {
		return nil
	}
	return c.Apps
}
