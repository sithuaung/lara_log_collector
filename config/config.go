package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the log collector
type Config struct {
	LogDirectory     string        `yaml:"log_directory"`
	AppName          string        `yaml:"app_name"`
	MinLogLevel      string        `yaml:"min_log_level"`
	MessageMaxLength int           `yaml:"message_max_length"`
	Lark             LarkConfig    `yaml:"lark"`
	Buffer           BufferConfig  `yaml:"buffer"`
	Watcher          WatcherConfig `yaml:"watcher"`
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
	PollInterval time.Duration `yaml:"poll_interval"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		LogDirectory:     "./storage/logs",
		AppName:          "Laravel Logs",
		MinLogLevel:      "ERROR",
		MessageMaxLength: 50,
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
			PollInterval: 1 * time.Second,
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
	if v := os.Getenv("LOG_DIRECTORY"); v != "" {
		c.LogDirectory = v
	}
	if v := os.Getenv("APP_NAME"); v != "" {
		c.AppName = v
	}
	if v := os.Getenv("LARK_WEBHOOK_URL"); v != "" {
		c.Lark.WebhookURL = v
	}
	if v := os.Getenv("MIN_LOG_LEVEL"); v != "" {
		c.MinLogLevel = v
	}
}
