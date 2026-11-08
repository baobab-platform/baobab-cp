package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/peoplebinding"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

const peopleProviderReadScope = "people:provider:read"

var peopleCorrelationID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type peopleChoiceReader interface {
	ReadPeopleChoice(context.Context, string, string, string, string, time.Time) (peoplebinding.Choice, error)
}
type peopleSelectionHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	store      peopleChoiceReader
	resolution service.CapabilityResolutionService
	clientIDs  map[string]bool
	now        func() time.Time
}
type peopleSelectionRequest struct {
	ContextID               string `json:"context_id"`
	CapabilityKey           string `json:"capability_key"`
	CorrelationID           string `json:"correlation_id"`
	RequiredContractVersion int    `json:"required_contract_version"`
}

func parsePeopleSelectionRequest(raw []byte) (peopleSelectionRequest, bool) {
	var req peopleSelectionRequest
	if len(raw) == 0 || len(raw) > 4096 {
		return req, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return req, false
	}
	seen := map[string]bool{}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		k, err := dec.Token()
		name, ok := k.(string)
		if err != nil || !ok || seen[name] {
			return req, false
		}
		seen[name] = true
		switch name {
		case "context_id", "capability_key", "correlation_id", "required_contract_version":
		default:
			return req, false
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return req, false
		}
		fields[name] = value
	}
	if k, err := dec.Token(); err != nil || k != json.Delim('}') {
		return req, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return req, false
	}
	if len(seen) != 4 || json.Unmarshal(fields["context_id"], &req.ContextID) != nil ||
		json.Unmarshal(fields["capability_key"], &req.CapabilityKey) != nil ||
		json.Unmarshal(fields["correlation_id"], &req.CorrelationID) != nil ||
		json.Unmarshal(fields["required_contract_version"], &req.RequiredContractVersion) != nil ||
		req.ContextID == "" || req.RequiredContractVersion != 1 || !peoplebinding.Supported(req.CapabilityKey) ||
		!peopleCorrelationID.MatchString(req.CorrelationID) {
		return req, false
	}
	return req, true
}
func (h peopleSelectionHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || !h.clientIDs[p.ClientID] ||
		!p.HasScope(peopleProviderReadScope) || !p.HasScope("context:resolve") ||
		h.api == nil || h.api.workloadRegistry == nil ||
		!h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, "PEOPLE_WORKLOAD_DENIED", "People provider read authority denied", false)
		return
	}
	registry, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !registry.AllowsScope(p.ClientID, peopleProviderReadScope) || !registry.AllowsScope(p.ClientID, "context:resolve") {
		problem(w, r, http.StatusForbidden, "PEOPLE_WORKLOAD_DENIED", "People workload scope not registered", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	req, ok := parsePeopleSelectionRequest(raw)
	if !ok {
		problem(w, r, http.StatusBadRequest, "PEOPLE_REQUEST_INVALID", "Invalid People provider selection request", false)
		return
	}
	trusted, ok := redeemContext(w, r, h.contexts, h.identities, req.ContextID)
	if !ok {
		return
	}
	if trusted.LegalEntityID == "" || trusted.Jurisdiction == "" || trusted.ID == "" {
		problem(w, r, http.StatusForbidden, "PEOPLE_EMPLOYER_CONTEXT_REQUIRED", "Exact employer and jurisdiction are required", false)
		return
	}
	if h.store == nil || h.resolution.Store == nil {
		problem(w, r, http.StatusServiceUnavailable, "PEOPLE_SELECTION_UNAVAILABLE", "People binding reader unavailable", true)
		return
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now().UTC()
	}
	selected, err := h.store.ReadPeopleChoice(r.Context(), trusted.TenantID, trusted.LegalEntityID, trusted.Jurisdiction, req.CapabilityKey, now)
	if err != nil {
		problem(w, r, http.StatusForbidden, "PEOPLE_PROVIDER_NOT_QUALIFIED", "No qualified selected People provider", false)
		return
	}
	resolution, err := h.resolution.Resolve(r.Context(), service.CapabilityResolutionRequest{
		Context: trusted, CapabilityKey: req.CapabilityKey, RequiredContractVersion: 1, CorrelationID: req.CorrelationID,
	})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PEOPLE_SELECTION_UNAVAILABLE", "Current CP authority unavailable", true)
		return
	}
	// Re-read after capability/grant/health resolution to minimise acceptance
	// of a revoked/cutover selection. Consequential adapter operations must
	// still recheck current authority immediately before side effects.
	again, err := h.store.ReadPeopleChoice(r.Context(), trusted.TenantID, trusted.LegalEntityID, trusted.Jurisdiction, req.CapabilityKey, now)
	if err != nil || !peoplebinding.SameChoice(again, selected) {
		problem(w, r, http.StatusForbidden, "PEOPLE_PROVIDER_CHANGED", "Provider choice changed during resolution", false)
		return
	}
	decision, err := peoplebinding.Build(trusted, selected, peoplebinding.Resolution{
		ResolutionID: resolution.ResolutionID, ContextID: resolution.ContextID,
		TenantID: resolution.TenantID, CapabilityKey: resolution.CapabilityKey,
		Decision: resolution.Decision, BindingID: resolution.BindingID,
		ProviderID: resolution.ProviderID, ContractVersion: resolution.ContractVersion,
		ExpiresAt: resolution.ExpiresAt,
	}, now)
	if err != nil {
		problem(w, r, http.StatusForbidden, "PEOPLE_PROVIDER_NOT_QUALIFIED", "Provider choice does not match current CP authority", false)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}
