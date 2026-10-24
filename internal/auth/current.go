package auth

import (
	"context"
	"errors"
	"time"
)

// CurrentVerifier adds issuer-native current status to locally verified claims.
// It does not mint identity, tenant or grant authority. Every request rechecks;
// unavailable introspection fails closed and is distinct from an inactive token.
type CurrentVerifier struct {
	Verifier     TokenVerifier
	Introspector Introspector
	Audience     string
}

func (v CurrentVerifier) Verify(ctx context.Context, token string) (Principal, error) {
	p, err := v.Verifier.Verify(ctx, token)
	if err != nil {
		return Principal{}, err
	}
	if v.Introspector == nil {
		return Principal{}, ErrIntrospectionUnavailable
	}
	current, err := v.Introspector.Introspect(ctx, token)
	if err != nil {
		return Principal{}, ErrIntrospectionUnavailable
	}
	audienceMatches := false
	for _, audience := range current.Audience {
		audienceMatches = audienceMatches || audience == v.Audience
	}
	effectiveScopes := map[string]struct{}{}
	for scope := range current.Scopes {
		if _, protocol := protocolScopes[scope]; !protocol {
			effectiveScopes[scope] = struct{}{}
		}
	}
	denied := errors.New("issuer token is not current")
	if !audienceMatches || !current.Active || current.Subject != p.Subject || current.ClientID != p.ClientID || current.Issuer != p.Issuer ||
		!current.ExpiresAt.After(time.Now()) || !current.ExpiresAt.Equal(p.ExpiresAt) || len(effectiveScopes) != len(p.Scopes) {
		return Principal{}, denied
	}
	for scope := range p.Scopes {
		if _, ok := effectiveScopes[scope]; !ok {
			return Principal{}, denied
		}
	}
	return p, nil
}

// ConfigureCurrentVerifier refuses partial configuration rather than silently
// falling back to signature-only verification when current status was requested.
func ConfigureCurrentVerifier(base TokenVerifier, issuer, audience, endpoint, clientID, secret string) (TokenVerifier, error) {
	if endpoint == "" && clientID == "" && secret == "" {
		return base, nil
	}
	introspector, err := NewHTTPIntrospector(issuer, endpoint, clientID, secret)
	if err != nil {
		return nil, err
	}
	return CurrentVerifier{Verifier: base, Introspector: introspector, Audience: audience}, nil
}
