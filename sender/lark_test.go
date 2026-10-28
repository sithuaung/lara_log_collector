package sender

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

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

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLarkResponseErrors(t *testing.T) {
	for _, tc := range []struct {
		body string
		fail bool
	}{
		{`{"code":0}`, false}, {`{"StatusCode":0}`, false},
		{`{"code":9499}`, true}, {`{"StatusCode":19001}`, true}, {`not json`, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			s := NewLarkSender(config.LarkConfig{WebhookURL: "http://example.test"}, nil)
			s.client.Transport = responseTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			if err := s.sendCard(map[string]any{}); (err != nil) != tc.fail {
				t.Fatalf("error=%v want failure=%v", err, tc.fail)
			}
		})
	}
}

func TestPacingSharedByConcurrentCards(t *testing.T) {
	s := NewLarkSender(config.LarkConfig{WebhookURL: "http://example.test", MinSendInterval: 30 * time.Millisecond}, nil)
	var starts []time.Time
	s.client.Transport = responseTransport(func(*http.Request) (*http.Response, error) {
		starts = append(starts, time.Now())
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.sendCard(map[string]any{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < 28*time.Millisecond {
			t.Fatalf("requests only %v apart", gap)
		}
	}
}

func TestBuildCardsStaysUnderLarkSizeLimit(t *testing.T) {
	s := &LarkSender{}
	var entries []*models.LogEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, &models.LogEntry{
			Level:   models.LevelError,
			Message: fmt.Sprintf("SQLSTATE[22001] %d %s", i, strings.Repeat(`"x\`, 5000)),
		})
	}
	cards := s.buildCards(entries, "app")
	if len(cards) < 2 {
		t.Fatalf("got %d cards, want split", len(cards))
	}
	for _, c := range cards {
		if n := jsonSize(s.buildPayload(c)); n > 20*1024 {
			t.Fatalf("card is %d bytes", n)
		}
	}
}
