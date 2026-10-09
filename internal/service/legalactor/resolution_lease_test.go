package legalactor

import (
	"testing"
	"time"
)

func TestResolvedLegalActorValidityLease(t *testing.T) {
	r, m := testRequest(), testMandate()
	got := Resolve(r, []Candidate{m}, testAt)
	if got.Outcome != Authorized || got.ValidUntil == nil || !got.ValidUntil.Equal(testAt.Add(30*time.Second)) {
		t.Fatalf("must bound open-ended mandate: %+v", got)
	}
	approved := testAt.Add(time.Minute)
	m.ApprovedAt = &approved
	if got = Resolve(r, []Candidate{m}, testAt); got.Outcome != ActorNotVerified {
		t.Fatalf("must deny future approval: %+v", got)
	}
	m.ApprovedAt = nil
}
