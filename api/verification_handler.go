// ADR-BCP-023 gate OEV-03: the verification workflow routes. Contract:
// baobab-platform/shared contracts/control-plane/v1/openapi.yaml (the
// Verification operations) and contracts/evidence/v1.
//
// Every identity is the caller's: the Control Plane records who asserted,
// checked, decided or resolved, and refuses anyone checking or deciding a
// claim they asserted. Results and conclusions need verification:decide.
// Evidence content is never served.
package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

var (
	caseCreateSchema            = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/VerificationCaseCreateRequest")
	caseTransitionSchema        = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/VerificationCaseTransitionRequest")
	claimSubmissionSchema       = contracts.MustSchema("evidence/v1/evidence.schema.json#/$defs/EvidenceClaimSubmission")
	checkRecordSchema           = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/VerificationCheckRecordRequest")
	resultRecordSchema          = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/VerificationResultRecordRequest")
	discrepancyRecordSchema     = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/EvidenceDiscrepancyRecordRequest")
	discrepancyTransitionSchema = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/EvidenceDiscrepancyTransitionRequest")
	caseConclusionSchema        = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/VerificationCaseConclusionRequest")
	discrepancyResolutionSchema = contracts.MustSchema("evidence/v1/verification.schema.json#/$defs/EvidenceDiscrepancyResolutionRequest")
	evidenceRegistrationSchema  = contracts.MustSchema("evidence/v1/evidence.schema.json#/$defs/EvidenceRegistrationRequest")
	verificationCaseIDPattern   = regexp.MustCompile(`^vcase_[a-z0-9]+$`)
	discrepancyIDPattern        = regexp.MustCompile(`^edis_[a-z0-9]+$`)
	evidenceIDPattern           = regexp.MustCompile(`^evr_[a-z0-9]+$`)
)

type verificationHandler struct {
	repo       repository.VerificationRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

func (h verificationHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

func (h verificationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrVerificationNotFound):
		problem(w, r, http.StatusNotFound, "VERIFICATION_NOT_FOUND", "no such verification record", false)
	case errors.Is(err, repository.ErrVerificationVersion):
		problem(w, r, http.StatusPreconditionFailed, "VERSION_MISMATCH", "the record changed since it was read", false)
	case errors.Is(err, repository.ErrVerificationState):
		problem(w, r, http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT", err.Error(), false)
	case errors.Is(err, repository.ErrVerificationInvalidToken):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "page_token is invalid", false)
	case errors.Is(err, verification.ErrSelfVerification):
		problem(w, r, http.StatusForbidden, "VERIFICATION_SELF_CHECK", "a claim is never checked or decided by its asserter", false)
	case errors.Is(err, verification.ErrUnknownSource):
		problem(w, r, http.StatusUnprocessableEntity, "EVIDENCE_SOURCE_UNKNOWN", err.Error(), false)
	case errors.Is(err, verification.ErrUnsupported):
		problem(w, r, http.StatusUnprocessableEntity, "VERIFICATION_UNSUPPORTED", err.Error(), false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "VERIFICATION_UNAVAILABLE", "the verification record could not be processed", true)
	}
}

func pathID(w http.ResponseWriter, r *http.Request, param string, pattern *regexp.Regexp) (string, bool) {
	id := chi.URLParam(r, param)
	if len(id) > 63 || !pattern.MatchString(id) {
		problem(w, r, http.StatusNotFound, "VERIFICATION_NOT_FOUND", "no such verification record", false)
		return "", false
	}
	return id, true
}

// decode reads and validates the request body against schema.
func (h verificationHandler) decode(w http.ResponseWriter, r *http.Request, schema *contracts.Schema, into any) ([]byte, bool) {
	raw, ok := readBody(w, r)
	if !ok {
		return nil, false
	}
	return raw, decodeRaw(w, r, schema, raw, into)
}

type itemsPage[T any] struct {
	Items         []T    `json:"items"`
	NextPageToken string `json:"next_page_token,omitempty"`
}

// --- Cases ---------------------------------------------------------------

func (h verificationHandler) createCase(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var req struct {
		Subject        verification.Subject `json:"subject"`
		Purpose        string               `json:"purpose"`
		ApplicationID  string               `json:"application_id"`
		OrganisationID string               `json:"organisation_id"`
		PolicyProfile  string               `json:"policy_profile"`
		PolicyVersion  int                  `json:"policy_version"`
		Supersedes     string               `json:"supersedes"`
	}
	raw, ok := h.decode(w, r, caseCreateSchema, &req)
	if !ok {
		return
	}
	hash := sha256Hex(raw)
	ctx := r.Context()
	if existing, prior, err := h.repo.GetVerificationCaseByIdempotencyKey(ctx, principalID, key); err == nil {
		h.replayCase(w, r, existing, prior, hash)
		return
	} else if !errors.Is(err, repository.ErrVerificationNotFound) {
		h.fail(w, r, err)
		return
	}
	c := verification.Case{CaseID: domain.NewResourceID("vcase"), Subject: req.Subject, ApplicationID: req.ApplicationID,
		OrganisationID: req.OrganisationID, Purpose: req.Purpose, PolicyProfile: req.PolicyProfile, PolicyVersion: req.PolicyVersion,
		Status: "DRAFT", ClaimIDs: []string{}, OpenedAt: h.clock(), Supersedes: req.Supersedes, CorrelationID: actor.CorrelationID, Version: 1}
	created, err := h.repo.OpenVerificationCase(ctx, c, principalID, key, hash, actor)
	if errors.Is(err, repository.ErrVerificationIdempotency) {
		existing, prior, getErr := h.repo.GetVerificationCaseByIdempotencyKey(ctx, principalID, key)
		if getErr != nil {
			h.fail(w, r, getErr)
			return
		}
		h.replayCase(w, r, existing, prior, hash)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/verification-cases/"+created.CaseID)
	w.Header().Set("ETag", entityTag(created.Version))
	writeJSON(w, http.StatusCreated, created)
}

func (h verificationHandler) replayCase(w http.ResponseWriter, r *http.Request, existing verification.Case, prior, hash string) {
	if prior != hash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/admin/verification-cases/"+existing.CaseID)
	w.Header().Set("ETag", entityTag(existing.Version))
	writeJSON(w, http.StatusCreated, existing)
}

func (h verificationHandler) listCases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.VerificationCaseFilter{Status: q.Get("status"), SubjectID: q.Get("subject_id"), PageToken: q.Get("page_token")}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "limit must be between 1 and 100", false)
			return
		}
		f.Limit = n
	}
	if f.Status != "" && !verificationCaseStatuses[f.Status] {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "status is not a verification case status", false)
		return
	}
	if len(f.SubjectID) > 128 {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "subject_id is too long", false)
		return
	}
	items, next, err := h.repo.ListVerificationCases(r.Context(), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if items == nil {
		items = []verification.Case{}
	}
	writeJSON(w, http.StatusOK, itemsPage[verification.Case]{Items: items, NextPageToken: next})
}

var verificationCaseStatuses = map[string]bool{"DRAFT": true, "COLLECTING_EVIDENCE": true, "READY_FOR_REVIEW": true, "VERIFYING": true,
	"INFORMATION_REQUIRED": true, "CONFLICTED": true, "VERIFIED": true, "NOT_VERIFIED": true, "CANCELLED": true, "SUPERSEDED": true, "EXPIRED": true}

func (h verificationHandler) getCase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	c, err := h.repo.GetVerificationCase(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(c.Version))
	writeJSON(w, http.StatusOK, c)
}

// transitionCase serves the working commands; concludeCase the deciding
// ones, under verification:decide. The request schemas keep them apart.
func (h verificationHandler) transitionCase(w http.ResponseWriter, r *http.Request) {
	h.caseCommand(w, r, caseTransitionSchema)
}

func (h verificationHandler) concludeCase(w http.ResponseWriter, r *http.Request) {
	h.caseCommand(w, r, caseConclusionSchema)
}

func (h verificationHandler) caseCommand(w http.ResponseWriter, r *http.Request, schema *contracts.Schema) {
	id, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	version, ok := versionIfMatch(w, r)
	if !ok {
		return
	}
	_, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var t verification.Transition
	if _, ok := h.decode(w, r, schema, &t); !ok {
		return
	}
	c, err := h.repo.TransitionVerificationCase(r.Context(), id, version, t, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(c.Version))
	writeJSON(w, http.StatusOK, c)
}

// --- Claims, checks, results and discrepancies ---------------------------

func (h verificationHandler) addClaim(w http.ResponseWriter, r *http.Request) {
	caseID, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var sub verification.ClaimSubmission
	if _, ok := h.decode(w, r, claimSubmissionSchema, &sub); !ok {
		return
	}
	cl, err := h.repo.AddVerificationClaim(r.Context(), caseID, sub, domain.NewResourceID("ecl"), principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, cl)
}

func (h verificationHandler) listClaims(w http.ResponseWriter, r *http.Request) {
	listFor(h, w, r, h.repo.ListVerificationClaims)
}

func (h verificationHandler) recordCheck(w http.ResponseWriter, r *http.Request) {
	caseID, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var rec verification.CheckRecord
	if _, ok := h.decode(w, r, checkRecordSchema, &rec); !ok {
		return
	}
	chk, err := h.repo.RecordVerificationCheck(r.Context(), caseID, rec, domain.NewResourceID("vchk"), principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, chk)
}

func (h verificationHandler) listChecks(w http.ResponseWriter, r *http.Request) {
	listFor(h, w, r, h.repo.ListVerificationChecks)
}

func (h verificationHandler) recordResult(w http.ResponseWriter, r *http.Request) {
	caseID, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var rec verification.ResultRecord
	if _, ok := h.decode(w, r, resultRecordSchema, &rec); !ok {
		return
	}
	res, err := h.repo.RecordVerificationResult(r.Context(), caseID, rec, domain.NewResourceID("vres"), principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (h verificationHandler) listResults(w http.ResponseWriter, r *http.Request) {
	listFor(h, w, r, h.repo.ListVerificationResults)
}

func (h verificationHandler) recordDiscrepancy(w http.ResponseWriter, r *http.Request) {
	caseID, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	_, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var rec verification.DiscrepancyRecord
	if _, ok := h.decode(w, r, discrepancyRecordSchema, &rec); !ok {
		return
	}
	d, err := h.repo.RecordEvidenceDiscrepancy(r.Context(), caseID, rec, domain.NewResourceID("edis"), h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(d.Version))
	writeJSON(w, http.StatusCreated, d)
}

func (h verificationHandler) listDiscrepancies(w http.ResponseWriter, r *http.Request) {
	listFor(h, w, r, h.repo.ListEvidenceDiscrepancies)
}

// transitionDiscrepancy serves the working commands; resolveDiscrepancy
// the closing ones, under verification:decide.
func (h verificationHandler) transitionDiscrepancy(w http.ResponseWriter, r *http.Request) {
	h.discrepancyCommand(w, r, discrepancyTransitionSchema)
}

func (h verificationHandler) resolveDiscrepancy(w http.ResponseWriter, r *http.Request) {
	h.discrepancyCommand(w, r, discrepancyResolutionSchema)
}

func (h verificationHandler) discrepancyCommand(w http.ResponseWriter, r *http.Request, schema *contracts.Schema) {
	id, ok := pathID(w, r, "discrepancyID", discrepancyIDPattern)
	if !ok {
		return
	}
	version, ok := versionIfMatch(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var t verification.Transition
	if _, ok := h.decode(w, r, schema, &t); !ok {
		return
	}
	d, err := h.repo.TransitionEvidenceDiscrepancy(r.Context(), id, version, t, principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(d.Version))
	writeJSON(w, http.StatusOK, d)
}

func listFor[T any](h verificationHandler, w http.ResponseWriter, r *http.Request, list func(ctx context.Context, caseID string) ([]T, error)) {
	caseID, ok := pathID(w, r, "caseID", verificationCaseIDPattern)
	if !ok {
		return
	}
	items, err := list(r.Context(), caseID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemsPage[T]{Items: items})
}

// --- Evidence and sources ------------------------------------------------

func (h verificationHandler) registerEvidence(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var req verification.EvidenceRegistration
	raw, ok := h.decode(w, r, evidenceRegistrationSchema, &req)
	if !ok {
		return
	}
	hash := sha256Hex(raw)
	obtainer := "principal:" + principalID
	ctx := r.Context()
	if existing, prior, err := h.repo.GetEvidenceByIdempotencyKey(ctx, obtainer, key); err == nil {
		h.replayEvidence(w, r, existing, prior, hash)
		return
	} else if !errors.Is(err, repository.ErrVerificationNotFound) {
		h.fail(w, r, err)
		return
	}
	e := verification.Evidence{EvidenceID: domain.NewResourceID("evr"), EvidenceType: req.EvidenceType, Subject: req.Subject,
		Purpose: req.Purpose, SourceID: req.SourceID, SourceRecordReference: req.SourceRecordReference,
		CredentialReference: req.CredentialReference, ObtainedBy: obtainer, IssuedAt: req.IssuedAt, ObservedAt: req.ObservedAt,
		ExpiresAt: req.ExpiresAt, Classification: req.Classification, RetentionPolicy: req.RetentionPolicy,
		Supersedes: req.Supersedes, CreatedAt: h.clock(), Version: 1}
	// Material that is not an uploaded file is accepted without quarantine.
	status, err := verification.Next(verification.MachineEvidence, "RECEIVED", "accept", verification.ActorPlatform)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	e.Status = status
	created, err := h.repo.RegisterEvidence(ctx, e, key, hash, actor)
	if errors.Is(err, repository.ErrVerificationIdempotency) {
		existing, prior, getErr := h.repo.GetEvidenceByIdempotencyKey(ctx, obtainer, key)
		if getErr != nil {
			h.fail(w, r, getErr)
			return
		}
		h.replayEvidence(w, r, existing, prior, hash)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/evidence/"+created.EvidenceID)
	writeJSON(w, http.StatusCreated, created)
}

func (h verificationHandler) replayEvidence(w http.ResponseWriter, r *http.Request, existing verification.Evidence, prior, hash string) {
	if prior != hash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/admin/evidence/"+existing.EvidenceID)
	writeJSON(w, http.StatusCreated, existing)
}

// getEvidence serves an evidence record's metadata only (section 270).
func (h verificationHandler) getEvidence(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "evidenceID", evidenceIDPattern)
	if !ok {
		return
	}
	e, err := h.repo.GetEvidence(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e.Reference())
}

func (h verificationHandler) listSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, itemsPage[verification.Source]{Items: verification.Sources()})
}

// versionIfMatch reads Shared's VersionIfMatch, or writes 428 or 400.
func versionIfMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		problem(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match with the record's current version is required", false)
		return 0, false
	}
	if !strongRevisionTag.MatchString(value) {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the record's version as a strong entity tag, e.g. \"3\"", false)
		return 0, false
	}
	version, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the record's version, e.g. \"3\"", false)
		return 0, false
	}
	return version, true
}
