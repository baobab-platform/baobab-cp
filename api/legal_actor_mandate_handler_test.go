package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

type inertMandateStore struct{}

func (inertMandateStore) ProposeOperatingLegalActorMandate(context.Context, string, basestore.RequestMetadata, string, legalactor.ProposeCommand) (legalactor.CommandReceipt,error) {
	return legalactor.CommandReceipt{}, errors.New("handler must not reach repository without authorization")
}
func (inertMandateStore) DecideOperatingLegalActorMandate(context.Context, string, basestore.RequestMetadata, string, string, legalactor.DecideCommand) (legalactor.CommandReceipt,error) {
	return legalactor.CommandReceipt{}, errors.New("handler must not reach repository without authorization")
}

func TestLA04CMandateRoutesDefaultOff(t *testing.T) {
	handler := New(Dependencies{Store: &fakeStore{}})
	for _, url := range []string{
		"/v2/legal-actor-mandates",
		"/v2/legal-actor-mandates/0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/decision",
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost,url,strings.NewReader("{}")))
		if w.Code != http.StatusNotFound {
			t.Errorf("default-disabled mandate endpoint %s returned %d",url,w.Code)
		}
	}
}

func TestLA04CMandateEndpointsRequireAdminToken(t *testing.T) {
	handler := New(Dependencies{
		Store: &fakeStore{}, LegalActorMandatesV2: true, LegalActorMandates: inertMandateStore{},
		AdminVerifier: fakeVerifier{err: errors.New("invalid bearer")},
	})
	for _, url := range []string{
		"/v2/legal-actor-mandates",
		"/v2/legal-actor-mandates/0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/decision",
	} {
		req := httptest.NewRequest(http.MethodPost,url,strings.NewReader("{}"))
		req.Header.Set("Authorization","Bearer invalid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w,req)
		if w.Code!=http.StatusUnauthorized {
			t.Errorf("mandate endpoint %s allowed invalid bearer: %d",url,w.Code)
		}
	}
}
