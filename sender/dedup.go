package sender

import (
	"sort"
	"strings"
	"time"

	"github.com/sithuaung/lara_log_collector/models"
)

type dedupState struct {
	windowStart time.Time
	suppressed  int
	latest      *models.LogEntry
}

// deduper sends the first occurrence of an app/level/message immediately,
// holds back repeats for the window, then sends one summary with their count.
type deduper struct {
	window time.Duration
	seen   map[string]*dedupState
}

func newDeduper(window time.Duration) *deduper {
	return &deduper{window: window, seen: make(map[string]*dedupState)}
}

func dedupKey(e *models.LogEntry) string {
	return e.AppName + "|" + string(e.Level) + "|" + messageKey(e.Message)
}

// messageKey drops Laravel's trailing JSON context (userId, exception path...)
// so the same error from different users or releases matches.
func messageKey(message string) string {
	if i := strings.Index(message, ` {"`); i > 0 {
		return message[:i]
	}
	return message
}

// filter returns the entries to send now. force flushes every pending summary (used on shutdown).
func (d *deduper) filter(entries []*models.LogEntry, now time.Time, force bool) []*models.LogEntry {
	if d.window <= 0 {
		return entries
	}

	out := make([]*models.LogEntry, 0, len(entries))
	for _, e := range entries {
		k := dedupKey(e)
		st, ok := d.seen[k]
		if ok && now.Sub(st.windowStart) < d.window {
			st.suppressed++
			st.latest = e
			continue
		}
		if ok && st.suppressed > 0 {
			// Window closed but not yet swept: fold held-back repeats into this send.
			e.Occurrences = st.suppressed + 1
		}
		d.seen[k] = &dedupState{windowStart: now}
		out = append(out, e)
	}

	var summaries []*models.LogEntry
	for k, st := range d.seen {
		if !force && now.Sub(st.windowStart) < d.window {
			continue
		}
		if st.suppressed == 0 {
			delete(d.seen, k)
			continue
		}
		summary := *st.latest
		summary.Occurrences = st.suppressed
		summaries = append(summaries, &summary)
		if force {
			delete(d.seen, k)
		} else {
			// Keep the key in a fresh window so ongoing repeats roll up again.
			d.seen[k] = &dedupState{windowStart: now}
		}
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Timestamp.Before(summaries[j].Timestamp) })

	return append(out, summaries...)
}
