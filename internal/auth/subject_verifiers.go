package auth

import (
	"context"
	"fmt"
	"sync"
)

// SubjectVerifiers returns the verifier for a subject token presented to the
// Control Plane by a validator (POST /platform-context/validate). The subject
// token is addressed to the validator's own audience, not to the Control
// Plane, so the Control Plane's request verifier cannot check it.
type SubjectVerifiers interface {
	// For returns a verifier that accepts only tokens addressed to audience.
	For(ctx context.Context, audience string) (TokenVerifier, error)
}

// AudienceVerifiers builds one OIDC verifier per audience on first use and
// keeps it. A discovery failure is not cached, so a transient outage heals.
type AudienceVerifiers struct {
	Issuer string

	mu        sync.Mutex
	verifiers map[string]TokenVerifier
}

// For implements SubjectVerifiers.
func (v *AudienceVerifiers) For(ctx context.Context, audience string) (TokenVerifier, error) {
	if audience == "" {
		return nil, fmt.Errorf("audience is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if verifier, ok := v.verifiers[audience]; ok {
		return verifier, nil
	}
	verifier, err := NewOIDCVerifier(ctx, v.Issuer, audience)
	if err != nil {
		return nil, err
	}
	if v.verifiers == nil {
		v.verifiers = map[string]TokenVerifier{}
	}
	v.verifiers[audience] = verifier
	return verifier, nil
}
