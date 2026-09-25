package sender

import (
	"testing"

	"github.com/sithuaung/lara_log_collector/config"
	"github.com/sithuaung/lara_log_collector/models"
)

func TestBuildCardKeepsCompleteParsedMessage(t *testing.T) {
	s := &LarkSender{
		cfg: config.LarkConfig{},
	}
	longMessage := "complete error details"
	card := s.buildCard([]*models.LogEntry{{
		Level:       models.LevelError,
		Environment: "production",
		Message:     longMessage,
	}}, "example-app")

	elements := card["elements"].([]map[string]any)
	message := elements[1]["text"].(map[string]any)["content"].(string)
	if message != longMessage {
		t.Fatalf("message = %q, want %q", message, longMessage)
	}
}
