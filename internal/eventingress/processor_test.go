package eventingress

import (
	"context"
	"errors"
	"testing"
	"time"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func processorRig(t *testing.T, apply Apply) (*Processor, *MemoryInbox, *clock, Event) {
	t.Helper()
	policy, err := LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{now: instant}
	inbox := &MemoryInbox{Clock: c.Now}
	body := provisioningEvent(t, nil)
	event := Event{Source: erpSource, ID: "1d05e706-89eb-51bb-a643-686aa039df63", Type: erpType, TenantID: "tn_01k4m7x9q2v6c8r3d5f1h0j4", KeyID: "k", Body: body, BodySHA256: "0"}
	if _, err := inbox.Accept(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	return &Processor{Inbox: inbox, Policy: policy, Handlers: map[string]Apply{erpType: apply}, Now: c.Now}, inbox, c, event
}

func TestARecordedEventIsAppliedWithOnlyItsDataAndThenLeftAlone(t *testing.T) {
	var got []string
	p, inbox, _, event := processorRig(t, func(_ context.Context, data []byte) error { got = append(got, string(data)); return nil })
	summary, err := p.RunOnce(context.Background())
	if err != nil || summary.Applied != 1 || inbox.Status(event.Source, event.ID) != "applied" {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	if len(got) != 1 || got[0][0] != '{' || !contains(got[0], `"operation_id"`) || contains(got[0], `"specversion"`) {
		t.Fatalf("the handler must receive the data member only: %v", got)
	}
	if again, _ := p.RunOnce(context.Background()); again.Applied != 0 || len(got) != 1 {
		t.Fatal("an applied event was applied again")
	}
}

func TestAnEventTheHandlerCannotYetActOnWaitsWithBackoffThenIsDeadLetteredAtTheirPolicyAge(t *testing.T) {
	calls := 0
	p, inbox, c, event := processorRig(t, func(context.Context, []byte) error {
		calls++
		return errors.New("the ERP operation was not submitted by this control plane")
	})
	summary, _ := p.RunOnce(context.Background())
	if summary.Retried != 1 || inbox.Status(event.Source, event.ID) != "pending" || inbox.Attempts(event.Source, event.ID) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	// Not due yet: backoff is real, and nothing is attempted.
	if again, _ := p.RunOnce(context.Background()); again.Retried != 0 || calls != 1 {
		t.Fatalf("an event that is not due was attempted (calls=%d)", calls)
	}
	// Due again after the first backoff.
	c.now = c.now.Add(6 * time.Second)
	if _, err := p.RunOnce(context.Background()); err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	// Still failing at the policy age (24 hours since it was received): dead-lettered, not retried forever.
	c.now = instant.Add(24 * time.Hour)
	summary, _ = p.RunOnce(context.Background())
	if summary.DeadLettered != 1 || inbox.Status(event.Source, event.ID) != "dead_letter" {
		t.Fatalf("summary=%+v status=%s", summary, inbox.Status(event.Source, event.ID))
	}
}

func TestAnEventThatBecomesApplicableIsAppliedOnTheNextPassEvenAfterRetries(t *testing.T) {
	known := false
	p, inbox, c, event := processorRig(t, func(context.Context, []byte) error {
		if !known {
			return errors.New("the ERP operation was not submitted by this control plane")
		}
		return nil
	})
	_, _ = p.RunOnce(context.Background())
	known = true
	c.now = c.now.Add(time.Minute)
	if summary, _ := p.RunOnce(context.Background()); summary.Applied != 1 || inbox.Status(event.Source, event.ID) != "applied" {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestAPermanentFailureIsDeadLetteredAtOnce(t *testing.T) {
	p, inbox, _, event := processorRig(t, func(context.Context, []byte) error {
		return Permanent(errors.New("the ERP state disagrees with the submitted request"))
	})
	summary, _ := p.RunOnce(context.Background())
	if summary.DeadLettered != 1 || inbox.Status(event.Source, event.ID) != "dead_letter" || inbox.Attempts(event.Source, event.ID) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if !errors.Is(Permanent(errors.New("x")), errors.Unwrap(Permanent(errors.New("x")))) && Permanent(nil) != nil {
		t.Fatal("Permanent(nil) must be nil")
	}
}

func TestAnEventWhoseTypeHasNoHandlerIsLeftUntouched(t *testing.T) {
	p, inbox, c, event := processorRig(t, func(context.Context, []byte) error { return nil })
	p.Handlers = map[string]Apply{}
	c.now = instant.Add(72 * time.Hour) // even past its policy age, nothing decides about an event nobody is configured to apply
	summary, err := p.RunOnce(context.Background())
	if err != nil || summary != (Summary{}) || inbox.Status(event.Source, event.ID) != "pending" || inbox.Attempts(event.Source, event.ID) != 0 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	if backoff(1) != 5*time.Second || backoff(2) != 10*time.Second || backoff(3) != 20*time.Second {
		t.Fatalf("%v %v %v", backoff(1), backoff(2), backoff(3))
	}
	if backoff(50) != 10*time.Minute {
		t.Fatalf("not capped: %v", backoff(50))
	}
}

func TestProcessedReceiptsAreKeptAtLeastTheRetentionAndPendingOnesNever(t *testing.T) {
	p, inbox, c, event := processorRig(t, func(context.Context, []byte) error { return nil })
	_, _ = p.RunOnce(context.Background())
	pending := Event{Source: erpSource, ID: "22222222-2222-4222-8222-222222222222", Type: "com.baobab-platform.erp.invoice.changed.v1", TenantID: "tn_x", KeyID: "k", Body: []byte(`{}`), BodySHA256: "1"}
	_, _ = inbox.Accept(context.Background(), pending)
	c.now = instant.Add(13 * 24 * time.Hour) // less than twice the 7 day retention
	p.lastPurge = time.Time{}
	_, _ = p.RunOnce(context.Background())
	if inbox.Status(event.Source, event.ID) != "applied" {
		t.Fatal("a receipt was forgotten before its retention")
	}
	c.now = instant.Add(15 * 24 * time.Hour)
	p.lastPurge = time.Time{}
	_, _ = p.RunOnce(context.Background())
	if inbox.Status(event.Source, event.ID) != "" {
		t.Fatal("an old processed receipt was never forgotten")
	}
	if inbox.Status(pending.Source, pending.ID) != "pending" {
		t.Fatal("a pending receipt must never be purged")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
