package api

import (
	"errors"
	"net/http"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/capability/certification"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
	"github.com/go-chi/chi/v5"
)

var (
	providerCertificationRecordSchema = contracts.MustSchema(
		"capability/v1/certification.schema.json#/$defs/ProviderCapabilityCertificationRecordRequest",
	)
	providerCertificationRevocationSchema = contracts.MustSchema(
		"capability/v1/certification.schema.json#/$defs/ProviderCapabilityCertificationRevocationRequest",
	)
)

type providerCapabilityCertificationHandler struct {
	repo       repository.ProviderCapabilityCertificationRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

func (h providerCapabilityCertificationHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}

func (h providerCapabilityCertificationHandler) record(w http.ResponseWriter, r *http.Request) {
	certifiedBy, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req certification.RecordRequest
	if !decodeRaw(w, r, providerCertificationRecordSchema, raw, &req) {
		return
	}
	if err := req.Check(h.clock()); err != nil {
		problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	recorded, replay, err := h.repo.RecordProviderCapabilityCertification(
		r.Context(), req, certifiedBy, h.clock(), actor,
	)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/provider-capability-certifications/"+recorded.CertificationID)
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(w, status, recorded)
}

func (h providerCapabilityCertificationHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := repository.ProviderCapabilityCertificationFilter{
		ProviderID:    q.Get("provider_id"),
		CapabilityKey: q.Get("capability_key"),
		ReleaseID:     q.Get("release_id"),
		Status:        q.Get("status"),
		PageToken:     q.Get("page_token"),
	}
	if filter.ProviderID != "" && !domain.ValidProviderID(filter.ProviderID) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "provider_id is invalid", false)
		return
	}
	if filter.CapabilityKey != "" && !capabilitydomain.ValidCapabilityKey(filter.CapabilityKey) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "capability_key is invalid", false)
		return
	}
	if filter.ReleaseID != "" && !release.ValidID(filter.ReleaseID) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "release_id is invalid", false)
		return
	}
	if filter.Status != "" && filter.Status != certification.StatusCertified && filter.Status != certification.StatusRevoked {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "status is invalid", false)
		return
	}
	items, next, err := h.repo.ListProviderCapabilityCertifications(r.Context(), filter)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page := certification.Page{Items: items}
	if next != "" {
		page.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, page)
}

func (h providerCapabilityCertificationHandler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetProviderCapabilityCertification(
		r.Context(),
		chi.URLParam(r, "certificationID"),
	)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h providerCapabilityCertificationHandler) revoke(w http.ResponseWriter, r *http.Request) {
	revokedBy, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req certification.RevocationRequest
	if !decodeRaw(w, r, providerCertificationRevocationSchema, raw, &req) {
		return
	}
	item, err := h.repo.RevokeProviderCapabilityCertification(
		r.Context(),
		chi.URLParam(r, "certificationID"),
		req,
		revokedBy,
		h.clock(),
		actor,
	)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h providerCapabilityCertificationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrCertificationNotFound):
		problem(w, r, http.StatusNotFound, "PROVIDER_CERTIFICATION_NOT_FOUND", "no provider capability certification has that id", false)
	case errors.Is(err, repository.ErrCertificationProviderNotFound):
		problem(w, r, http.StatusNotFound, "CAPABILITY_PROVIDER_NOT_FOUND", "no capability provider has that id", false)
	case errors.Is(err, repository.ErrCertificationCapabilityNotFound):
		problem(w, r, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "no canonical capability has that key", false)
	case errors.Is(err, repository.ErrCertificationReleaseNotFound):
		problem(w, r, http.StatusNotFound, "ENGINE_RELEASE_NOT_FOUND", "no engine release has that id", false)
	case errors.Is(err, repository.ErrCertificationSelf):
		problem(w, r, http.StatusForbidden, "CERTIFICATION_MAKER_CHECKER_REQUIRED", "the release recorder cannot certify that release", false)
	case errors.Is(err, repository.ErrCertificationConflict):
		problem(w, r, http.StatusConflict, "PROVIDER_CERTIFICATION_CONFLICT", "a different current certification already exists", false)
	case errors.Is(err, repository.ErrCertificationAlreadyRevoked):
		problem(w, r, http.StatusConflict, "PROVIDER_CERTIFICATION_ALREADY_REVOKED", "the certification is already revoked", false)
	case errors.Is(err, repository.ErrCertificationReleaseRevoked):
		problem(w, r, http.StatusConflict, "ENGINE_RELEASE_REVOKED", "a revoked engine release cannot be certified", false)
	case errors.Is(err, repository.ErrCertificationProviderReleaseMismatch):
		problem(w, r, http.StatusUnprocessableEntity, "CERTIFICATION_ENGINE_MISMATCH", err.Error(), false)
	case errors.Is(err, repository.ErrCertificationProviderSupportMissing):
		problem(w, r, http.StatusUnprocessableEntity, "CERTIFICATION_PROVIDER_SUPPORT_MISSING", err.Error(), false)
	case errors.Is(err, repository.ErrCertificationReleaseSupportMissing):
		problem(w, r, http.StatusUnprocessableEntity, "CERTIFICATION_RELEASE_SUPPORT_MISSING", err.Error(), false)
	case errors.Is(err, repository.ErrCertificationMalformedPageToken):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "page_token is malformed", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_CERTIFICATION_UNAVAILABLE", "provider certification could not be processed", true)
	}
}
