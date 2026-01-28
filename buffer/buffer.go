package buffer

import (
	"sync"
	"sync/atomic"

	"github.com/sithuaung/lara_log_collector/models"
)

// Buffer is a bounded buffer for log entries with backpressure handling
type Buffer struct {
	entries    chan *models.LogEntry
	size       int
	dropOldest bool

	// Metrics
	dropped  atomic.Int64
	received atomic.Int64

	mu sync.Mutex
}

// NewBuffer creates a new bounded buffer
func NewBuffer(size int, dropOldest bool) *Buffer {
	return &Buffer{
		entries:    make(chan *models.LogEntry, size),
		size:       size,
		dropOldest: dropOldest,
	}
}

// Push adds a log entry to the buffer
// If buffer is full and dropOldest is true, drops the oldest entry
// If dropOldest is false, blocks until space is available
func (b *Buffer) Push(entry *models.LogEntry) {
	b.received.Add(1)

	if b.dropOldest {
		b.pushWithDrop(entry)
	} else {
		b.entries <- entry
	}
}

// pushWithDrop attempts non-blocking push, dropping oldest if full
func (b *Buffer) pushWithDrop(entry *models.LogEntry) {
	select {
	case b.entries <- entry:
		// Successfully added
	default:
		// Buffer full, drop oldest and retry
		b.mu.Lock()
		select {
		case <-b.entries:
			b.dropped.Add(1)
		default:
		}
		b.mu.Unlock()

		// Try again (should succeed now)
		select {
		case b.entries <- entry:
		default:
			// Still full, drop this entry
			b.dropped.Add(1)
		}
	}
}

// Pop retrieves a log entry from the buffer (blocks if empty)
func (b *Buffer) Pop() *models.LogEntry {
	return <-b.entries
}

// TryPop attempts to retrieve a log entry without blocking
func (b *Buffer) TryPop() (*models.LogEntry, bool) {
	select {
	case entry := <-b.entries:
		return entry, true
	default:
		return nil, false
	}
}

// Channel returns the underlying channel for use with select
func (b *Buffer) Channel() <-chan *models.LogEntry {
	return b.entries
}

// Len returns the current number of entries in the buffer
func (b *Buffer) Len() int {
	return len(b.entries)
}

// Stats returns buffer statistics
func (b *Buffer) Stats() (received, dropped int64, currentLen int) {
	return b.received.Load(), b.dropped.Load(), len(b.entries)
}

// Close closes the buffer channel
func (b *Buffer) Close() {
	close(b.entries)
}
