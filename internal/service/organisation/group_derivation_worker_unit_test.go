package organisation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// unreadableGroups fails every derivation at its first read.
type unreadableGroups struct {
	repository.OrganisationRepository
}

func (unreadableGroups) GetCorporateGroup(context.Context, string) (*domain.CorporateGroup, error) {
	return nil, errors.New("registry unavailable")
}

// fakeDerivationQueue is an in-memory GroupDerivationRepository for one
// group, honouring due times and the claim lease like the database does.
type fakeDerivationQueue struct {
	group      string
	owed       bool
	seq        int64
	attempts   int
	due        time.Time
	leased     time.Time
	lastError  string
	derivedSeq int64
}

func (q *fakeDerivationQueue) ClaimCorporateGroupDerivations(_ context.Context, now time.Time, lease time.Duration, _ int) ([]repository.GroupDerivationClaim, error) {
	if !q.owed || q.due.After(now) || q.leased.After(now) {
		return nil, nil
	}
	q.leased = now.Add(lease)
	return []repository.GroupDerivationClaim{{GroupID: q.group, RequestSeq: q.seq, Attempts: q.attempts}}, nil
}

func (q *fakeDerivationQueue) RecordCorporateGroupDerived(_ context.Context, _ string, seq int64, _ time.Time, _, _ int) error {
	q.attempts, q.lastError, q.derivedSeq, q.leased = 0, "", seq, time.Time{}
	q.owed = q.seq != seq
	return nil
}

func (q *fakeDerivationQueue) RecordCorporateGroupDerivationFailure(_ context.Context, _ string, _ int64, _ time.Time, reason string, next time.Time) error {
	q.attempts++
	q.lastError, q.due, q.leased = reason, next, time.Time{}
	return nil
}

func (q *fakeDerivationQueue) RequestCorporateGroupDerivations(context.Context, string) (int, error) {
	q.owed, q.seq = true, q.seq+1
	return 1, nil
}

func (q *fakeDerivationQueue) GetCorporateGroupDerivation(context.Context, string) (*repository.GroupDerivationState, error) {
	return nil, nil
}

// TestGroupDerivationWorkerRetries: a failed derivation is retried only
// after its backoff, which doubles per attempt up to the cap, and a failure
// never stops the pass or clears the request.
func TestGroupDerivationWorkerRetries(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	queue := &fakeDerivationQueue{group: "cgrp_0190a1b2c3d4e5f60718293a4b5c6d7e"}
	worker := &GroupDerivationWorker{Deriver: &CorporateGroupDeriver{Orgs: unreadableGroups{}}, Queue: queue,
		Now: func() time.Time { return now }, MaxBackoff: 4 * time.Minute}
	if _, err := queue.RequestCorporateGroupDerivations(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	for attempt, wantDelay := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 4 * time.Minute} {
		pass, err := worker.RunOnce(context.Background())
		if err != nil || pass.Failed != 1 || queue.attempts != attempt+1 || queue.lastError != "registry unavailable" || !queue.owed {
			t.Fatalf("attempt %d: pass %+v, err %v, queue %+v", attempt+1, pass, err, queue)
		}
		if got := queue.due.Sub(now); got != wantDelay {
			t.Fatalf("attempt %d: retry after %v, want %v", attempt+1, got, wantDelay)
		}
		now = now.Add(wantDelay - time.Second)
		if pass, _ := worker.RunOnce(context.Background()); pass.Failed != 0 {
			t.Fatalf("attempt %d was retried before its backoff elapsed", attempt+1)
		}
		now = now.Add(time.Second)
	}
}
