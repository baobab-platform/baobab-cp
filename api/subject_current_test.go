package api

import (
	"context"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"testing"
)

type currentSubjectSource struct{}

func (currentSubjectSource) For(context.Context, string) (auth.TokenVerifier, error) {
	return unavailableSubject{}, nil
}

type unavailableSubject struct{}

func (unavailableSubject) Verify(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrIntrospectionUnavailable
}
func TestSubjectCurrentStatusOutageIsUnavailable(t *testing.T) {
	h := ContextValidationHandler{Subjects: currentSubjectSource{}}
	_, status := h.verifySubject(context.Background(), []string{"baobab-cms"}, "private-bearer")
	if status != subjectUnavailable {
		t.Fatal("issuer status outage was misreported as invalid subject")
	}
}
