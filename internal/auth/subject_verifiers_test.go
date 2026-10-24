package auth

import (
	"context"
	"testing"
	"time"
)

func TestSubjectAudienceRechecksCurrentStatus(t *testing.T) {
	p := Principal{Issuer: "https://issuer.example", Subject: "cms-subject", ClientID: "consumer", ExpiresAt: time.Now().Add(time.Minute).Truncate(time.Second), Scopes: map[string]struct{}{"content:entry:resolve": {}}}
	active := Introspection{Active: true, Issuer: p.Issuer, Subject: p.Subject, ClientID: p.ClientID, ExpiresAt: p.ExpiresAt, Audience: []string{"baobab-cms"}, Scopes: p.Scopes}
	reader := &changingSubjectStatus{value: active}
	v := (&AudienceVerifiers{Introspector: reader}).current(currentBase{p: p}, "baobab-cms")
	if _, err := v.Verify(context.Background(), "same-bearer"); err != nil {
		t.Fatal(err)
	}
	reader.value.Active = false
	if _, err := v.Verify(context.Background(), "same-bearer"); err == nil {
		t.Fatal("revoked subject admitted by cached verifier")
	}
	reader.value = active
	reader.value.Audience = []string{"baobab-control-plane"}
	if _, err := v.Verify(context.Background(), "same-bearer"); err == nil {
		t.Fatal("validator audience substituted for subject audience")
	}
	reader.err = ErrIntrospectionUnavailable
	if _, err := v.Verify(context.Background(), "same-bearer"); err != ErrIntrospectionUnavailable {
		t.Fatal("status outage not preserved")
	}
}

type changingSubjectStatus struct {
	value Introspection
	err   error
}

func (s *changingSubjectStatus) Introspect(context.Context, string) (Introspection, error) {
	return s.value, s.err
}
