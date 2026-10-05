package sender

import (
	"fmt"
	"testing"
	"time"

	"github.com/sithuaung/lara_log_collector/models"
)

func authErr() *models.LogEntry {
	return &models.LogEntry{AppName: "bpe-api", Level: models.LevelError, Message: "Auth guard [internal] is not defined."}
}

func TestDedupHoldsRepeatsAndSummarisesAfterWindow(t *testing.T) {
	d := newDeduper(10 * time.Minute)
	t0 := time.Date(2026, 10, 5, 3, 24, 59, 0, time.UTC)

	if out := d.filter([]*models.LogEntry{authErr()}, t0, false); len(out) != 1 || out[0].Occurrences != 0 {
		t.Fatalf("first occurrence: got %+v", out)
	}
	if out := d.filter([]*models.LogEntry{authErr(), authErr()}, t0.Add(22*time.Second), false); len(out) != 0 {
		t.Fatalf("repeats inside window should be held, got %d", len(out))
	}
	other := &models.LogEntry{AppName: "bpe-api", Level: models.LevelError, Message: "different"}
	if out := d.filter([]*models.LogEntry{other}, t0.Add(time.Minute), false); len(out) != 1 {
		t.Fatalf("different error should send immediately, got %d", len(out))
	}

	out := d.filter(nil, t0.Add(10*time.Minute), false)
	if len(out) != 1 || out[0].Occurrences != 2 {
		t.Fatalf("summary after window: got %+v", out)
	}
	if out := d.filter(nil, t0.Add(30*time.Minute), false); len(out) != 0 {
		t.Fatalf("quiet window should send nothing, got %d", len(out))
	}
}

func TestDedupFoldsHeldRepeatsIntoNextSend(t *testing.T) {
	d := newDeduper(time.Minute)
	t0 := time.Now()
	d.filter([]*models.LogEntry{authErr()}, t0, false)
	d.filter([]*models.LogEntry{authErr()}, t0.Add(time.Second), false)

	out := d.filter([]*models.LogEntry{authErr()}, t0.Add(2*time.Minute), false)
	if len(out) != 1 || out[0].Occurrences != 2 {
		t.Fatalf("got %+v", out)
	}
}

func TestDedupForceFlushesPending(t *testing.T) {
	d := newDeduper(time.Hour)
	t0 := time.Now()
	d.filter([]*models.LogEntry{authErr(), authErr(), authErr()}, t0, false)
	if out := d.filter(nil, t0.Add(time.Second), true); len(out) != 1 || out[0].Occurrences != 2 {
		t.Fatalf("got %+v", out)
	}
}

func TestDedupDisabled(t *testing.T) {
	d := newDeduper(0)
	if out := d.filter([]*models.LogEntry{authErr(), authErr()}, time.Now(), false); len(out) != 2 {
		t.Fatalf("got %d", len(out))
	}
}

func TestDedupIgnoresContext(t *testing.T) {
	d := newDeduper(10 * time.Minute)
	t0 := time.Now()
	msg := `Auth guard [internal] is not defined. {"userId":%d,"exception":"..."}`
	a := authErr()
	a.Message = fmt.Sprintf(msg, 7)
	b := authErr()
	b.Message = fmt.Sprintf(msg, 54)
	if out := d.filter([]*models.LogEntry{a, b}, t0, false); len(out) != 1 {
		t.Fatalf("different userId should dedup, got %d", len(out))
	}
}
