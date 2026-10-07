package eventingress

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"time"
)

// Apply acts on one event's data. It is given only the data member of a recorded, verified envelope.
type Apply func(ctx context.Context, data []byte) error

type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }

// Permanent marks an error no retry can change: the event is dead-lettered at once.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

// Processor applies recorded events. Applying is at-least-once and idempotent: the receipt moves to applied only after the handler
// returns, and a handler must tolerate seeing an event again. An event the handler cannot yet act on (an operation the Control Plane
// has not recorded) stays pending with backoff until the policy age elapses, then is dead-lettered for operators.
type Processor struct {
	Inbox    Inbox
	Policy   Policy
	Handlers map[string]Apply
	Now      func() time.Time
	Batch    int

	lastPurge time.Time
}

// Summary is one pass.
type Summary struct{ Applied, Retried, DeadLettered int }

const (
	retryBase = 5 * time.Second
	retryCap  = 10 * time.Minute
)

func (p *Processor) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func backoff(attempts int) time.Duration {
	delay := retryBase
	for i := 1; i < attempts && delay < retryCap; i++ {
		delay *= 2
	}
	if delay > retryCap {
		delay = retryCap
	}
	return delay
}

// RunOnce applies every due event of a type that has a handler. Events whose type has no handler are left untouched.
func (p *Processor) RunOnce(ctx context.Context) (Summary, error) {
	var summary Summary
	types := make([]string, 0, len(p.Handlers))
	for eventType := range p.Handlers {
		types = append(types, eventType)
	}
	sort.Strings(types)
	if len(types) == 0 {
		return summary, nil
	}
	batch := p.Batch
	if batch <= 0 {
		batch = 100
	}
	due, err := p.Inbox.Due(ctx, p.now(), types, batch)
	if err != nil {
		return summary, err
	}
	for _, event := range due {
		attempts := event.Attempts + 1
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		err := json.Unmarshal(event.Body, &envelope)
		if err == nil {
			err = p.Handlers[event.Type](ctx, envelope.Data)
		}
		switch {
		case err == nil:
			if err := p.Inbox.MarkApplied(ctx, event.Source, event.ID); err != nil {
				return summary, err
			}
			summary.Applied++
		case isPermanent(err) || p.expired(event):
			reason := reasonOf(err)
			if markErr := p.Inbox.MarkDeadLetter(ctx, event.Source, event.ID, attempts, reason); markErr != nil {
				return summary, markErr
			}
			summary.DeadLettered++
			slog.WarnContext(ctx, "engine event dead-lettered", "event_type", event.Type, "event_id", event.ID, "attempts", attempts, "reason", reason)
		default:
			if markErr := p.Inbox.MarkRetry(ctx, event.Source, event.ID, attempts, reasonOf(err), backoff(attempts)); markErr != nil {
				return summary, markErr
			}
			summary.Retried++
		}
	}
	p.purge(ctx)
	return summary, nil
}

func (p *Processor) expired(event Pending) bool {
	allowed, ok := p.Policy.Lookup(event.Type)
	return ok && p.now().Sub(event.ReceivedAt) >= allowed.PendingMaxAge
}

// purge forgets processed receipts older than the retention, at most once an hour. Pending receipts are never purged.
func (p *Processor) purge(ctx context.Context) {
	if p.now().Sub(p.lastPurge) < time.Hour {
		return
	}
	p.lastPurge = p.now()
	if _, err := p.Inbox.Purge(ctx, p.now().Add(-2*p.Policy.Retention())); err != nil {
		slog.WarnContext(ctx, "event receipt purge failed", "error", err)
	}
}

// Run applies recorded events until ctx ends: on every interval and whenever wake receives (a delivery has just been recorded).
func (p *Processor) Run(ctx context.Context, interval time.Duration, wake <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := p.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "applying recorded engine events failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-wake:
		}
	}
}

func isPermanent(err error) bool {
	var permanent permanentError
	return errors.As(err, &permanent)
}

// reasonOf is a bounded, secret-free description of why an event was not applied: the error text of a Control Plane error, cut short.
func reasonOf(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 240 {
		text = text[:240]
	}
	return text
}
