package eventingress

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryInbox is an Inbox held in memory, for tests of this and other packages. It follows PostgresInbox's rules exactly (the same
// (source, id) is a Duplicate or a conflict, never a second record) and is safe for concurrent use.
type MemoryInbox struct {
	mu   sync.Mutex
	rows map[string]*memoryRow
	// Clock stamps received_at; time.Now when nil.
	Clock func() time.Time
	// FailAccept, when set, makes Accept fail (a store outage).
	FailAccept error
}

type memoryRow struct {
	Pending
	TenantID, KeyID, Digest, Status, LastError string
	NextAttempt                                time.Time
}

func (m *MemoryInbox) now() time.Time {
	if m.Clock != nil {
		return m.Clock()
	}
	return time.Now()
}

func (m *MemoryInbox) Accept(_ context.Context, ev Event) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailAccept != nil {
		return Receipt{}, m.FailAccept
	}
	if m.rows == nil {
		m.rows = map[string]*memoryRow{}
	}
	key := ev.Source + "|" + ev.ID
	if row, seen := m.rows[key]; seen {
		if row.Digest != ev.BodySHA256 {
			return Receipt{}, ErrConflict
		}
		return Receipt{EventID: ev.ID, Status: Duplicate, ReceivedAt: row.ReceivedAt}, nil
	}
	row := &memoryRow{Pending: Pending{Source: ev.Source, ID: ev.ID, Type: ev.Type, Body: append([]byte(nil), ev.Body...), ReceivedAt: m.now()},
		TenantID: ev.TenantID, KeyID: ev.KeyID, Digest: ev.BodySHA256, Status: "pending"}
	m.rows[key] = row
	return Receipt{EventID: ev.ID, Status: Accepted, ReceivedAt: row.ReceivedAt}, nil
}

func (m *MemoryInbox) Due(_ context.Context, now time.Time, types []string, limit int) ([]Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Pending
	for _, row := range m.rows {
		wanted := false
		for _, t := range types {
			wanted = wanted || t == row.Type
		}
		if row.Status == "pending" && wanted && !row.NextAttempt.After(now) {
			out = append(out, row.Pending)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReceivedAt.Before(out[j].ReceivedAt) || (out[i].ReceivedAt.Equal(out[j].ReceivedAt) && out[i].ID < out[j].ID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryInbox) set(source, id string, change func(*memoryRow)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[source+"|"+id]; ok && row.Status == "pending" {
		change(row)
	}
	return nil
}

func (m *MemoryInbox) MarkApplied(_ context.Context, source, id string) error {
	return m.set(source, id, func(r *memoryRow) { r.Status = "applied" })
}

func (m *MemoryInbox) MarkRetry(_ context.Context, source, id string, attempts int, reason string, delay time.Duration) error {
	return m.set(source, id, func(r *memoryRow) {
		r.Attempts, r.LastError, r.NextAttempt = attempts, reason, m.now().Add(delay)
	})
}

func (m *MemoryInbox) MarkDeadLetter(_ context.Context, source, id string, attempts int, reason string) error {
	return m.set(source, id, func(r *memoryRow) { r.Status, r.Attempts, r.LastError = "dead_letter", attempts, reason })
}

func (m *MemoryInbox) Purge(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for key, row := range m.rows {
		if row.Status != "pending" && row.ReceivedAt.Before(cutoff) {
			delete(m.rows, key)
			n++
		}
	}
	return n, nil
}

func (m *MemoryInbox) Backlog(context.Context) (Backlog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b Backlog
	for _, row := range m.rows {
		switch row.Status {
		case "pending":
			b.Pending++
		case "applied":
			b.Applied++
		default:
			b.DeadLetter++
		}
	}
	return b, nil
}

// Status of a recorded event, for tests: "" when absent.
func (m *MemoryInbox) Status(source, id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[source+"|"+id]; ok {
		return row.Status
	}
	return ""
}

// Attempts of a recorded event, for tests.
func (m *MemoryInbox) Attempts(source, id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[source+"|"+id]; ok {
		return row.Attempts
	}
	return 0
}

// Len is the number of recorded events.
func (m *MemoryInbox) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}
