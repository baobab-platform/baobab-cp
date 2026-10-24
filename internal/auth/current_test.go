package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type currentBase struct {
	p   Principal
	err error
}

func (v currentBase) Verify(context.Context, string) (Principal, error) { return v.p, v.err }

type currentStatus struct {
	status Introspection
	err    error
}

func (v currentStatus) Introspect(context.Context, string) (Introspection, error) {
	return v.status, v.err
}
func TestCurrentVerifierEnforcesIssuerStatusAndClaimBinding(t *testing.T) {
	p := Principal{Issuer: "https://issuer.example", Subject: "subject", ClientID: "client", ExpiresAt: time.Now().Add(time.Minute).Truncate(time.Second), Scopes: map[string]struct{}{"context:resolve": {}}}
	status := Introspection{Active: true, Issuer: p.Issuer, Subject: p.Subject, ClientID: p.ClientID, ExpiresAt: p.ExpiresAt, Audience: []string{"cp"}, Scopes: map[string]struct{}{"context:resolve": {}, "openid": {}}}
	for _, tc := range []struct {
		name   string
		change func(*Introspection)
	}{
		{"positive", func(*Introspection) {}}, {"revoked", func(x *Introspection) { x.Active = false }},
		{"wrong subject", func(x *Introspection) { x.Subject = "other" }}, {"wrong client", func(x *Introspection) { x.ClientID = "other" }},
		{"wrong issuer", func(x *Introspection) { x.Issuer = "https://other.example" }}, {"wrong audience", func(x *Introspection) { x.Audience = []string{"other"} }},
		{"changed expiry", func(x *Introspection) { x.ExpiresAt = x.ExpiresAt.Add(time.Minute) }},
		{"changed scopes", func(x *Introspection) { x.Scopes = map[string]struct{}{"other:scope": {}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := status
			tc.change(&x)
			v := CurrentVerifier{Verifier: currentBase{p: p}, Introspector: currentStatus{status: x}, Audience: "cp"}
			_, err := v.Verify(context.Background(), "private")
			if (err == nil) != (tc.name == "positive") {
				t.Fatal("current issuer status not enforced")
			}
		})
	}
	v := CurrentVerifier{Verifier: currentBase{p: p}, Introspector: currentStatus{err: errors.New("outage")}, Audience: "cp"}
	if _, err := v.Verify(context.Background(), "private"); !errors.Is(err, ErrIntrospectionUnavailable) {
		t.Fatal("outage confused with revocation")
	}
}
func TestPartialCurrentConfigurationCannotDowngrade(t *testing.T) {
	base := currentBase{}
	if v, err := ConfigureCurrentVerifier(base, "https://issuer.example", "cp", "", "", ""); err != nil || v == nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"", "http://issuer.example/introspect", "https://foreign.example/introspect"} {
		if _, err := ConfigureCurrentVerifier(base, "https://issuer.example", "cp", endpoint, "client", "01234567890123456789012345678901"); err == nil {
			t.Fatal("unsafe current configuration accepted")
		}
	}
}
