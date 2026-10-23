// ADR-SHARED-042 section 14, Decision 9 (CP ADR-BCP-031; Shared control-plane/v1
// estate-resource-assignment.openapi.yaml): governed estate resource
// assignments. Humans propose, approve (four-eyes), suspend, reinstate and
// revoke them; a registered Trade workload resolves a short-lived attestation
// that one non-BFF native commitment operation has governed Digital Estate
// provenance. CP verifies the subject itself: a WORKLOAD's own IAM token
// (verified and introspected), or a NATIVE_RESOURCE against the caller's own
// Trade-0 attestation. A Medusa key, sales channel or asserted estate is never
// provenance, and the attestation authorises nothing beyond it.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

const (
	estateAssignmentResolveScope = "estate:resource-assignment:resolve"
	estateAssignmentWriteScope   = "estate:resource-assignment:write"
	estateAssignmentApproveScope = "estate:resource-assignment:approve"
	// The server application's own credential for submitting native
	// commitments to Trade; identity, never authority.
	nativeCommitmentSubmitScope = "trade:native-commitment:submit"
	assignmentAttestationTTL    = 60 * time.Second
)

var (
	assignmentResolveSchema     = contracts.MustSchema("control-plane/v1/estate-resource-assignment.schema.json#/$defs/EstateResourceAssignmentResolveRequest")
	assignmentAttestationSchema = contracts.MustSchema("control-plane/v1/estate-resource-assignment.schema.json#/$defs/EstateResourceAssignmentAttestation")
	assignmentProposalSchema    = contracts.MustSchema("control-plane/v1/estate-resource-assignment.schema.json#/$defs/EstateResourceAssignmentProposal")
	assignmentTransitionSchema  = contracts.MustSchema("control-plane/v1/estate-resource-assignment.schema.json#/$defs/EstateResourceAssignmentTransition")
	assignmentRecordSchema      = contracts.MustSchema("control-plane/v1/estate-resource-assignment.schema.json#/$defs/EstateResourceAssignment")
)

type estateResourceAssignmentStore interface {
	ProposeEstateResourceAssignment(context.Context, string, basestore.RequestMetadata, string,
		postgres.EstateResourceAssignmentProposal) (postgres.EstateResourceAssignment, error)
	TransitionEstateResourceAssignment(context.Context, string, basestore.RequestMetadata, string, string,
		postgres.EstateResourceAssignmentTransition) (postgres.EstateResourceAssignment, error)
	EstateResourceAssignment(context.Context, string) (postgres.EstateResourceAssignment, error)
	EstateResourceAssignmentCandidates(ctx context.Context, scope, subject, estate string) ([]postgres.EstateResourceAssignment, error)
	NativeConfigurationAttested(ctx context.Context, caller, engine, reference, digest string) (bool, error)
	RecordEstateResourceAssignmentResolution(context.Context, postgres.EstateResourceAssignmentResolution) error
}

type assignmentResolveRequest struct {
	Scope                string              `json:"scope"`
	WorkloadEvidence     string              `json:"workload_evidence"`
	WorkloadEvidenceType string              `json:"workload_evidence_type"`
	NativeResource       *postgres.NativeRef `json:"native_resource"`
	DigitalEstateID      string              `json:"digital_estate_id"`
	EngineInstanceID     string              `json:"engine_instance_id"`
	MarketID             string              `json:"market_id"`
	Operation            string              `json:"operation"`
	CorrelationID        string              `json:"correlation_id"`
}

type assignmentAttestation struct {
	Outcome                  string     `json:"outcome"`
	AttestationID            string     `json:"attestation_id"`
	AssignmentID             string     `json:"assignment_id"`
	AssignmentRevision       int        `json:"assignment_revision"`
	Scope                    string     `json:"scope"`
	WorkloadClientID         string     `json:"workload_client_id,omitempty"`
	DigitalEstateID          string     `json:"digital_estate_id"`
	TenantID                 string     `json:"tenant_id"`
	TradeEngineInstanceID    string     `json:"trade_engine_instance_id"`
	MarketID                 string     `json:"market_id"`
	Operation                string     `json:"operation"`
	IssuedAt                 time.Time  `json:"issued_at"`
	ExpiresAt                time.Time  `json:"expires_at"`
	AssignmentEffectiveUntil *time.Time `json:"assignment_effective_until,omitempty"`
	EvidenceReference        string     `json:"evidence_reference"`
}

type estateResourceAssignmentHandler struct {
	api   *API
	store estateResourceAssignmentStore
	// clients verifies a server application's token against the configured
	// IAM issuer for the Trade audience; introspector asks whether it is current.
	clients      auth.SubjectVerifiers
	introspector auth.Introspector
	now          func() time.Time
}

var assignmentDenialStatus = map[string]int{
	"WORKLOAD_EVIDENCE_INVALID":     http.StatusForbidden,
	"NATIVE_RESOURCE_NOT_ATTESTED":  http.StatusForbidden,
	"ASSIGNMENT_NOT_FOUND":          http.StatusNotFound,
	"ASSIGNMENT_NOT_ACTIVE":         http.StatusForbidden,
	"OPERATION_NOT_PERMITTED":       http.StatusForbidden,
	"MARKET_NOT_PERMITTED":          http.StatusForbidden,
	"ENGINE_INSTANCE_NOT_PERMITTED": http.StatusForbidden,
	"ASSIGNMENT_AMBIGUOUS":          http.StatusConflict,
}

func (h estateResourceAssignmentHandler) deny(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	problem(w, r, status, code, detail, status == http.StatusServiceUnavailable)
}

// admitted requires an ACTIVE workload whose registry entry currently allows
// the resolve scope; the scope is allocated to no workload.
func (h estateResourceAssignmentHandler) admitted(p auth.Principal) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" || p.ClientID == "" ||
		!p.HasScope(estateAssignmentResolveScope) || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	return ok && scopes.AllowsScope(p.ClientID, estateAssignmentResolveScope)
}

// selectAssignment applies the resolution rules, in order, to every
// assignment of the verified subject and estate. Ambiguity is never resolved
// by order.
func selectAssignment(candidates []postgres.EstateResourceAssignment, req assignmentResolveRequest, now time.Time) (
	postgres.EstateResourceAssignment, string, int) {
	keep := func(in []postgres.EstateResourceAssignment, ok func(postgres.EstateResourceAssignment) bool) []postgres.EstateResourceAssignment {
		var out []postgres.EstateResourceAssignment
		for _, a := range in {
			if ok(a) {
				out = append(out, a)
			}
		}
		return out
	}
	if len(candidates) == 0 {
		return postgres.EstateResourceAssignment{}, "ASSIGNMENT_NOT_FOUND", 0
	}
	steps := []struct {
		denial string
		ok     func(postgres.EstateResourceAssignment) bool
	}{
		{"ENGINE_INSTANCE_NOT_PERMITTED", func(a postgres.EstateResourceAssignment) bool {
			return a.TradeEngineInstanceID == req.EngineInstanceID
		}},
		{"ASSIGNMENT_NOT_ACTIVE", func(a postgres.EstateResourceAssignment) bool {
			return a.Status == "ACTIVE" && !a.EffectiveFrom.After(now) && (a.EffectiveUntil == nil || now.Before(*a.EffectiveUntil))
		}},
		{"MARKET_NOT_PERMITTED", func(a postgres.EstateResourceAssignment) bool { return slices.Contains(a.MarketScope, req.MarketID) }},
		{"OPERATION_NOT_PERMITTED", func(a postgres.EstateResourceAssignment) bool {
			return slices.Contains(a.PermittedOperations, req.Operation)
		}},
	}
	for _, step := range steps {
		if candidates = keep(candidates, step.ok); len(candidates) == 0 {
			return postgres.EstateResourceAssignment{}, step.denial, 0
		}
	}
	if len(candidates) > 1 {
		return postgres.EstateResourceAssignment{}, "ASSIGNMENT_AMBIGUOUS", len(candidates)
	}
	return candidates[0], "ATTESTED", 1
}

func (h estateResourceAssignmentHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !h.admitted(p) {
		h.deny(w, r, http.StatusForbidden, "CALLER_NOT_PERMITTED", "the authenticated workload may not resolve estate resource assignments")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req assignmentResolveRequest
	// Schema messages could quote workload_evidence, which is a credential.
	if contracts.Validate(assignmentResolveSchema, raw) != nil || json.Unmarshal(raw, &req) != nil ||
		r.Header.Get("X-Correlation-ID") != req.CorrelationID {
		h.deny(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid estate resource assignment request")
		return
	}
	if h.store == nil || h.now == nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "estate resource assignment resolution is not available")
		return
	}
	now := h.now().UTC()
	record := postgres.EstateResourceAssignmentResolution{CallerClientID: p.ClientID, Scope: req.Scope,
		DigitalEstateID: req.DigitalEstateID, EngineInstanceID: req.EngineInstanceID, MarketID: req.MarketID,
		Operation: req.Operation, ResolvedAt: now, CorrelationID: req.CorrelationID}
	refuse := func(code, detail string) {
		record.Outcome = code
		h.audit(r, record)
		// A denial whose record fails still denies.
		_ = h.store.RecordEstateResourceAssignmentResolution(r.Context(), record)
		h.deny(w, r, assignmentDenialStatus[code], code, detail)
	}

	// The subject, verified by CP itself.
	var subject string
	evidenceExpiry := time.Time{}
	if req.Scope == "WORKLOAD" {
		record.SubjectReference = "workload-evidence"
		if h.clients == nil || h.introspector == nil {
			h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the workload evidence issuer is unavailable")
			return
		}
		verifier, err := h.clients.For(r.Context(), buyerSubjectAudience)
		if err != nil {
			h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the workload evidence issuer is unavailable")
			return
		}
		server, err := verifier.Verify(r.Context(), req.WorkloadEvidence)
		if err != nil || server.ActorType != "workload" || server.Issuer == "" || server.TokenID == "" ||
			server.ClientID == "" || !server.HasScope(nativeCommitmentSubmitScope) || !server.ExpiresAt.After(now) {
			refuse("WORKLOAD_EVIDENCE_INVALID", "the workload evidence could not be verified")
			return
		}
		record.SubjectReference = server.ClientID
		current, err := h.introspector.Introspect(r.Context(), req.WorkloadEvidence)
		if err != nil {
			h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the workload evidence's current status is unavailable")
			return
		}
		if !current.Active || current.ClientID != server.ClientID || current.Subject != server.Subject ||
			(current.Issuer != "" && current.Issuer != server.Issuer) ||
			(len(current.Audience) > 0 && !slices.Contains(current.Audience, buyerSubjectAudience)) ||
			(len(current.Scopes) > 0 && !hasScopeEntry(current.Scopes, nativeCommitmentSubmitScope)) ||
			(!current.ExpiresAt.IsZero() && !current.ExpiresAt.After(now)) {
			refuse("WORKLOAD_EVIDENCE_INVALID", "the workload evidence is not current")
			return
		}
		subject, evidenceExpiry = server.ClientID, server.ExpiresAt
		if !current.ExpiresAt.IsZero() && current.ExpiresAt.Before(evidenceExpiry) {
			evidenceExpiry = current.ExpiresAt
		}
	} else {
		record.SubjectReference = req.NativeResource.Reference
		attested, err := h.store.NativeConfigurationAttested(r.Context(), p.ClientID, req.EngineInstanceID,
			req.NativeResource.Reference, req.NativeResource.Digest)
		if err != nil {
			h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the native configuration record is unavailable")
			return
		}
		if !attested {
			refuse("NATIVE_RESOURCE_NOT_ATTESTED", "the native configuration is not currently attested by this caller")
			return
		}
		subject = req.NativeResource.Reference
	}

	candidates, err := h.store.EstateResourceAssignmentCandidates(r.Context(), req.Scope, subject, req.DigitalEstateID)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the assignment store is unavailable")
		return
	}
	if req.Scope == "NATIVE_RESOURCE" {
		// The assignment names the exact attested configuration, digest included.
		var exact []postgres.EstateResourceAssignment
		for _, a := range candidates {
			if a.NativeConfigurationDigest == req.NativeResource.Digest {
				exact = append(exact, a)
			}
		}
		candidates = exact
	}
	assignment, outcome, count := selectAssignment(candidates, req, now)
	record.CandidateCount = count
	if outcome != "ATTESTED" {
		refuse(outcome, "no single active assignment gives this operation estate provenance")
		return
	}

	issued := now.Truncate(time.Second)
	expires := now.Add(assignmentAttestationTTL)
	if assignment.EffectiveUntil != nil && assignment.EffectiveUntil.Before(expires) {
		expires = *assignment.EffectiveUntil
	}
	if !evidenceExpiry.IsZero() && evidenceExpiry.Before(expires) {
		expires = evidenceExpiry
	}
	expires = expires.UTC().Truncate(time.Second)
	if !expires.After(issued) {
		refuse("ASSIGNMENT_NOT_ACTIVE", "the assignment or its evidence is about to stop being effective")
		return
	}
	out := assignmentAttestation{Outcome: "ATTESTED", AttestationID: domain.NewUUIDv7(), AssignmentID: assignment.AssignmentID,
		AssignmentRevision: assignment.Revision, Scope: assignment.Scope, DigitalEstateID: assignment.DigitalEstateID,
		TenantID: assignment.TenantID, TradeEngineInstanceID: assignment.TradeEngineInstanceID, MarketID: req.MarketID,
		Operation: req.Operation, IssuedAt: issued, ExpiresAt: expires, EvidenceReference: assignment.EvidenceReference}
	if assignment.Scope == "WORKLOAD" {
		out.WorkloadClientID = assignment.WorkloadClientID
	}
	if assignment.EffectiveUntil != nil {
		until := assignment.EffectiveUntil.UTC()
		out.AssignmentEffectiveUntil = &until
	}
	encoded, err := json.Marshal(out)
	if err != nil || contracts.Validate(assignmentAttestationSchema, encoded) != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "estate resource assignment resolution is not available")
		return
	}
	record.Outcome, record.AttestationID = "ATTESTED", out.AttestationID
	record.AssignmentID, record.AssignmentRevision, record.TenantID = assignment.AssignmentID, assignment.Revision, assignment.TenantID
	record.ExpiresAt = &expires
	if err := h.store.RecordEstateResourceAssignmentResolution(r.Context(), record); err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the resolution could not be recorded")
		return
	}
	h.audit(r, record)
	writeJSON(w, http.StatusOK, out)
}

func (h estateResourceAssignmentHandler) audit(r *http.Request, record postgres.EstateResourceAssignmentResolution) {
	// Never the workload evidence or the caller's token.
	slog.InfoContext(r.Context(), "estate resource assignment resolution", "caller_client_id", record.CallerClientID,
		"scope", record.Scope, "subject", record.SubjectReference, "digital_estate_id", record.DigitalEstateID,
		"market_id", record.MarketID, "operation", record.Operation, "outcome", record.Outcome,
		"assignment_id", record.AssignmentID, "assignment_revision", record.AssignmentRevision,
		"correlation_id", record.CorrelationID)
}

// writeAssignment returns the governed record, which must satisfy the Shared contract.
func (h estateResourceAssignmentHandler) writeAssignment(w http.ResponseWriter, r *http.Request, status int,
	a postgres.EstateResourceAssignment) {
	encoded, err := json.Marshal(a)
	if err != nil || contracts.Validate(assignmentRecordSchema, encoded) != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the assignment could not be represented", false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, json.RawMessage(encoded))
}

func (h estateResourceAssignmentHandler) propose(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var body postgres.EstateResourceAssignmentProposal
	if !decodeContract(w, r, assignmentProposalSchema, &body) {
		return
	}
	actorID, _, ok := resolveActor(w, r, h.api.identities, false)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	meta := requestMetadata(r, principal)
	meta.ActorID = actorID
	a, err := h.store.ProposeEstateResourceAssignment(r.Context(), key, meta, actorID, body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeAssignment(w, r, http.StatusCreated, a)
}

func (h estateResourceAssignmentHandler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.store.EstateResourceAssignment(r.Context(), chi.URLParam(r, "assignmentID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeAssignment(w, r, http.StatusOK, a)
}

func (h estateResourceAssignmentHandler) transition(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var body postgres.EstateResourceAssignmentTransition
	if !decodeContract(w, r, assignmentTransitionSchema, &body) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	// Approval-type changes need the approve scope, the others the write scope.
	required := estateAssignmentWriteScope
	if body.Action == "APPROVE" || body.Action == "REINSTATE" {
		required = estateAssignmentApproveScope
	}
	if !principal.HasScope(required) {
		problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
		return
	}
	actorID, _, ok := resolveActor(w, r, h.api.identities, false)
	if !ok {
		return
	}
	meta := requestMetadata(r, principal)
	meta.ActorID = actorID
	a, err := h.store.TransitionEstateResourceAssignment(r.Context(), key, meta, actorID, chi.URLParam(r, "assignmentID"), body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeAssignment(w, r, http.StatusOK, a)
}

func (h estateResourceAssignmentHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, basestore.ErrIdempotencyConflict):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "a different assignment command used this key", false)
	case errors.Is(err, postgres.ErrAssignmentNotFound):
		problem(w, r, http.StatusNotFound, "ASSIGNMENT_NOT_FOUND", "no such assignment", false)
	case errors.Is(err, postgres.ErrAssignmentSelfApproval):
		problem(w, r, http.StatusForbidden, "SELF_APPROVAL", "the approver must not be the proposer", false)
	case errors.Is(err, postgres.ErrAssignmentRevisionConflict):
		problem(w, r, http.StatusConflict, "REVISION_CONFLICT", "the assignment is not at the expected revision", false)
	case errors.Is(err, postgres.ErrAssignmentTerminal):
		problem(w, r, http.StatusConflict, "ASSIGNMENT_TERMINAL", "a revoked assignment cannot change", false)
	case errors.Is(err, postgres.ErrAssignmentOverlap):
		problem(w, r, http.StatusConflict, "ASSIGNMENT_OVERLAP", "another active assignment already covers this subject", false)
	case errors.Is(err, postgres.ErrAssignmentInvalidTransition):
		problem(w, r, http.StatusConflict, "INVALID_TRANSITION", "the transition is not governed from the current status", false)
	case errors.Is(err, postgres.ErrAssignmentEstateNotGoverned):
		problem(w, r, http.StatusConflict, "ESTATE_NOT_GOVERNED", "the estate has no single governed Tenant", false)
	case errors.Is(err, postgres.ErrAssignmentInvalid):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the assignment command is not valid", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "ASSIGNMENT_UNAVAILABLE", "the assignment registry is unavailable", true)
	}
}
