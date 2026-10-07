package eventingress

import (
	"context"
	"errors"
	"time"
)

// Status is what a receive answered: the first receipt of an event, or a redelivery of one already recorded with identical content.
type Status string

const (
	Accepted  Status = "ACCEPTED"
	Duplicate Status = "DUPLICATE"
)

// ErrConflict is an event whose (source, id) was already received with different content.
var ErrConflict = errors.New("an event with this source and id was already received with different content")

// Event is one verified, validated event as received.
type Event struct {
	Source     string
	ID         string
	Type       string
	TenantID   string
	KeyID      string
	Body       []byte
	BodySHA256 string
}

// Receipt answers a receive.
type Receipt struct {
	EventID    string
	Status     Status
	ReceivedAt time.Time
}

// Pending is a recorded event still to be applied.
type Pending struct {
	Source     string
	ID         string
	Type       string
	Body       []byte
	Attempts   int
	ReceivedAt time.Time
}

// Inbox is the durable receipt store.
type Inbox interface {
	// Accept records the event exactly once and says whether it was new. The same (source, id) with the same digest is a Duplicate;
	// with another digest it is ErrConflict.
	Accept(ctx context.Context, ev Event) (Receipt, error)
	// Due returns pending events of the given types whose next attempt has come, oldest first.
	Due(ctx context.Context, now time.Time, types []string, limit int) ([]Pending, error)
	MarkApplied(ctx context.Context, source, id string) error
	// MarkRetry keeps the event pending and schedules its next attempt.
	MarkRetry(ctx context.Context, source, id string, attempts int, reason string, delay time.Duration) error
	MarkDeadLetter(ctx context.Context, source, id string, attempts int, reason string) error
	// Purge forgets processed receipts received before cutoff. Pending receipts are never purged.
	Purge(ctx context.Context, cutoff time.Time) (int64, error)
	// Backlog counts receipts by state.
	Backlog(ctx context.Context) (Backlog, error)
}

// Backlog is the inbox as an operator needs to see it.
type Backlog struct {
	Pending, Applied, DeadLetter int64
	OldestPendingSeconds         int64
}
