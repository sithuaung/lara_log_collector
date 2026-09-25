package sender

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sithuaung/lara_log_collector/buffer"
	"github.com/sithuaung/lara_log_collector/config"
	"github.com/sithuaung/lara_log_collector/models"
)

type blockingTransport struct {
	started chan string
	release chan struct{}
}

func (tr *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	tr.started <- string(body)
	<-tr.release
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
}

func TestSlowSenderKeepsBacklogInBoundedBuffer(t *testing.T) {
	buf := buffer.NewBuffer(2, true)
	s := NewLarkSender(config.LarkConfig{
		WebhookURL: "http://example.test", BatchSize: 1, FlushInterval: time.Hour,
	}, buf)
	tr := &blockingTransport{started: make(chan string, 10), release: make(chan struct{})}
	s.client.Transport = tr
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Start(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		close(tr.release)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("sender did not stop")
		}
	})
	push := func(message string) { buf.Push(&models.LogEntry{Message: message, Level: models.LevelError}) }
	push("first")
	select {
	case <-tr.started:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	push("oldest")
	push("newer")
	push("newest")
	select {
	case <-tr.started:
		t.Fatal("another batch started while the first request was blocked")
	case <-time.After(50 * time.Millisecond):
	}
	_, dropped, pending := buf.Stats()
	if dropped != 1 || pending != 2 {
		t.Fatalf("dropped=%d pending=%d; want 1 and 2", dropped, pending)
	}
	tr.release <- struct{}{}
	select {
	case body := <-tr.started:
		if !strings.Contains(body, "newer") {
			t.Fatalf("expected oldest queued alert to be dropped; got %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("sender did not resume after request completed")
	}
}
