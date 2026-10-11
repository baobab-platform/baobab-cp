// PEO-02T: current negative-only documentary remediation policy decision.
// A nonblocked response NEVER means legal, provider, payment or trade approval.
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type documentaryRestrictionReader interface {
	DocumentaryScopeBlocked(context.Context, string, string, string) (bool, error)
}

type documentaryRestrictionRequest struct {
	ContextID          string `json:"context_id"`
	Capability         string `json:"capability"`
	Market             string `json:"market"`
	OperationReference string `json:"operation_reference"`
}

type documentaryRestrictionResponse struct {
	ContextID             string    `json:"context_id"`
	OperationReference    string    `json:"operation_reference"`
	OrganisationID        string    `json:"organisation_id"`
	Capability            string    `json:"capability"`
	Market                string    `json:"market"`
	Blocked               bool      `json:"blocked"`
	AuthorityGranted      bool      `json:"authority_granted"`
	PolicyReference       string    `json:"policy_reference"`
	EvaluatedAt           time.Time `json:"evaluated_at"`
	ExpiresAt             time.Time `json:"expires_at"`
}

var documentaryCapabilityKey = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,199}$`)
var documentaryMarketCode = regexp.MustCompile(`^[A-Z]{2}$`)

// The cache policy must wrap IAM middleware, since invalid credentials and
// missing scopes are rejected before the business handler is invoked.
func documentaryRestrictionNoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

type documentaryRestrictionHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	reader     documentaryRestrictionReader
	clientIDs  map[string]bool
}

func (h documentaryRestrictionHandler) assess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" ||
		!h.clientIDs[p.ClientID] || !p.HasScope("documentary:restriction-assess") ||
		h.api == nil || h.api.workloadRegistry == nil ||
		!h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, "DOCUMENTARY_RESTRICTION_CALLER_DENIED",
			"active registered documentary restriction consumer required", false)
		return
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, "documentary:restriction-assess") {
		problem(w, r, http.StatusForbidden, "DOCUMENTARY_RESTRICTION_CALLER_DENIED",
			"workload registration does not authorize this assessment", false)
		return
	}
	if h.reader == nil || h.contexts == nil || h.identities == nil || h.api.store == nil {
		problem(w, r, http.StatusServiceUnavailable, "DOCUMENTARY_RESTRICTION_UNAVAILABLE",
			"restriction authority is not configured", true)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 4096)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req documentaryRestrictionRequest
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF ||
		!domain.IsUUID(req.ContextID) ||
		!documentaryCapabilityKey.MatchString(req.Capability) ||
		!documentaryMarketCode.MatchString(req.Market) ||
		len(strings.TrimSpace(req.OperationReference)) < 3 ||
		len(req.OperationReference) > 256 {
		problem(w, r, http.StatusBadRequest, "INVALID_DOCUMENTARY_RESTRICTION_REQUEST",
			"bounded context, capability, market and operation are required", false)
		return
	}
	trusted, ok := redeemContext(w, r, h.contexts, h.identities, req.ContextID)
	if !ok {
		return
	}
	now := time.Now().UTC()
	if !trusted.IsRuntime() || trusted.ExpiresAt == nil ||
		!now.Before(*trusted.ExpiresAt) || trusted.ResolvedAt.After(now) ||
		!domain.IsUUID(trusted.OrganisationID) || trusted.CountryCode != req.Market {
		problem(w, r, http.StatusForbidden, "DOCUMENTARY_RESTRICTION_CONTEXT_DENIED",
			"live runtime operating Organisation and market required", false)
		return
	}
	tenant, err := h.api.store.GetTenant(r.Context(), trusted.TenantID)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "DOCUMENTARY_RESTRICTION_UNAVAILABLE",
			"tenant status unavailable", true)
		return
	}
	if tenant.ObservedState != string(domain.LifecycleActive) ||
		tenant.PrimaryOrganisationID != trusted.OrganisationID {
		problem(w, r, http.StatusForbidden, "DOCUMENTARY_RESTRICTION_CONTEXT_DENIED",
			"active tenant primary Organisation mismatch", false)
		return
	}
	blocked, err := h.reader.DocumentaryScopeBlocked(r.Context(),
		trusted.OrganisationID, req.Capability, req.Market)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "DOCUMENTARY_RESTRICTION_UNAVAILABLE",
			"current restriction status unavailable", true)
		return
	}
	expiry := now.Add(2 * time.Second)
	if trusted.ExpiresAt.Before(expiry) {
		expiry = *trusted.ExpiresAt
	}
	writeJSON(w, http.StatusOK, documentaryRestrictionResponse{
		ContextID: trusted.ID, OperationReference: req.OperationReference,
		OrganisationID: trusted.OrganisationID,
		Capability: req.Capability, Market: req.Market,
		Blocked: blocked, AuthorityGranted: false,
		PolicyReference: "ADR-BCP-026/PEO-02T:negative-only",
		EvaluatedAt: now, ExpiresAt: expiry,
	})
}
