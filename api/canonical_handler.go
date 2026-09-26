package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/go-chi/chi/v5"
)

// canonicalHandler serves the Canonical registry operations of Shared's
// control-plane/v1 OpenAPI. Its request and response bodies are
// control-plane/v1 canonical-entity.schema.json's CanonicalEntityCreateRequest
// and CanonicalEntity, kept apart from domain.CanonicalEntity so a change to
// the domain or its persistence cannot change the wire contract.
type canonicalHandler struct {
	service service.CanonicalEntityService
}

var (
	canonicalKeyPattern       = regexp.MustCompile(`^[a-z0-9]+:[a-z0-9][a-z0-9._:-]*$`)
	canonicalEntityTypeRegexp = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	canonicalAuthorityPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// canonicalEntityCreateRequest is CanonicalEntityCreateRequest. It has no
// identifier, status, version or timestamps: the Control Plane owns them, and
// the decoder refuses a body naming them.
type canonicalEntityCreateRequest struct {
	CanonicalKey       string     `json:"canonical_key"`
	EntityType         string     `json:"entity_type"`
	Subtype            *string    `json:"subtype"`
	DisplayName        string     `json:"display_name"`
	OwnerTenantID      *string    `json:"owner_tenant_id"`
	OwnerLegalEntityID *string    `json:"owner_legal_entity_id"`
	Authority          string     `json:"authority"`
	Classification     string     `json:"classification"`
	EffectiveFrom      *time.Time `json:"effective_from"`
	EffectiveTo        *time.Time `json:"effective_to"`
}

// validate applies the schema's structural rules (400). The validity window's
// order is semantic and is checked separately (422).
func (req canonicalEntityCreateRequest) validate() string {
	switch {
	case len(req.CanonicalKey) < 3 || len(req.CanonicalKey) > 255 || !canonicalKeyPattern.MatchString(req.CanonicalKey):
		return "canonical_key must use the namespace:type pattern"
	case len(req.EntityType) > 64 || !canonicalEntityTypeRegexp.MatchString(req.EntityType):
		return "entity_type must be an upper-case registered kind"
	case req.Subtype != nil && (strings.TrimSpace(*req.Subtype) == "" || len(*req.Subtype) > 64):
		return "subtype must be 1 to 64 characters"
	case strings.TrimSpace(req.DisplayName) == "" || len(req.DisplayName) > 255:
		return "display_name must be 1 to 255 characters"
	case req.OwnerTenantID != nil && !domain.ValidTenantID(*req.OwnerTenantID):
		return "owner_tenant_id is not a Control Plane tenant identifier"
	case req.OwnerLegalEntityID != nil && !domain.ValidLegalEntityID(*req.OwnerLegalEntityID):
		return "owner_legal_entity_id is not a legal entity identifier"
	case len(req.Authority) > 64 || !canonicalAuthorityPattern.MatchString(req.Authority):
		return "authority must be a lower-case domain name"
	}
	switch req.Classification {
	case "PUBLIC", "INTERNAL", "TENANT_CONFIDENTIAL", "RESTRICTED":
		return ""
	default:
		return "classification must be PUBLIC, INTERNAL, TENANT_CONFIDENTIAL or RESTRICTED"
	}
}

// canonicalEntityResponse is CanonicalEntity. A field the entity was
// registered without is omitted, never filled in.
type canonicalEntityResponse struct {
	ID                 string     `json:"id"`
	CanonicalKey       string     `json:"canonical_key,omitempty"`
	EntityType         string     `json:"entity_type"`
	Subtype            string     `json:"subtype,omitempty"`
	DisplayName        string     `json:"display_name,omitempty"`
	OwnerTenantID      string     `json:"owner_tenant_id,omitempty"`
	OwnerLegalEntityID string     `json:"owner_legal_entity_id,omitempty"`
	Authority          string     `json:"authority,omitempty"`
	Classification     string     `json:"classification,omitempty"`
	Status             string     `json:"status"`
	SchemaVersion      int        `json:"schema_version,omitempty"`
	EffectiveFrom      *time.Time `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time `json:"effective_to,omitempty"`
	Version            int64      `json:"version"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func newCanonicalEntityResponse(entity domain.CanonicalEntity) canonicalEntityResponse {
	out := canonicalEntityResponse{
		ID: entity.ID, CanonicalKey: entity.CanonicalKey, EntityType: entity.EntityType, Subtype: entity.Subtype,
		DisplayName: entity.DisplayName, OwnerTenantID: entity.OwnerTenantID, OwnerLegalEntityID: entity.OwnerLegalEntityID,
		Authority: entity.Authority, Classification: entity.Classification, Status: entity.Status,
		SchemaVersion: entity.SchemaVersion, Version: entity.Version,
		CreatedAt: entity.CreatedAt.UTC(), UpdatedAt: entity.UpdatedAt.UTC(),
	}
	if !entity.EffectiveFrom.IsZero() {
		from := entity.EffectiveFrom.UTC()
		out.EffectiveFrom = &from
	}
	if entity.EffectiveTo != nil {
		to := entity.EffectiveTo.UTC()
		out.EffectiveTo = &to
	}
	return out
}

func (h canonicalHandler) create(w http.ResponseWriter, r *http.Request) {
	var req canonicalEntityCreateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			problem(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body exceeds 1 MiB", false)
			return
		}
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not a valid canonical entity registration", false)
		return
	}
	if reason := req.validate(); reason != "" {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", reason, false)
		return
	}
	effectiveFrom := time.Now().UTC()
	if req.EffectiveFrom != nil {
		effectiveFrom = req.EffectiveFrom.UTC()
	}
	if req.EffectiveTo != nil && !req.EffectiveTo.After(effectiveFrom) {
		problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "effective_to must be after effective_from", false)
		return
	}
	entity := domain.CanonicalEntity{
		// CanonicalEntity is a first-class control-plane resource and MUST use
		// a UUIDv7 minted by Go (BCP-DB-001/BCP-GO-001 sections 4, 30-31).
		ID:           domain.NewUUIDv7(),
		CanonicalKey: req.CanonicalKey, EntityType: req.EntityType, DisplayName: req.DisplayName,
		Authority: req.Authority, Classification: req.Classification, Status: "DRAFT",
		SchemaVersion: 1, EffectiveFrom: effectiveFrom, EffectiveTo: req.EffectiveTo,
	}
	if req.Subtype != nil {
		entity.Subtype = *req.Subtype
	}
	if req.OwnerTenantID != nil {
		entity.OwnerTenantID = *req.OwnerTenantID
	}
	if req.OwnerLegalEntityID != nil {
		entity.OwnerLegalEntityID = *req.OwnerLegalEntityID
	}
	if _, err := h.service.Create(r.Context(), entity); err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "canonical entity registration failed", true)
		return
	}
	stored, err := h.service.Get(r.Context(), entity.ID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "canonical entity registration could not be read back", true)
		return
	}
	w.Header().Set("Location", "/v1/canonical-entities/"+stored.ID)
	w.Header().Set("ETag", entityTag(stored.Version))
	writeJSON(w, http.StatusCreated, newCanonicalEntityResponse(stored))
}

func (h canonicalHandler) get(w http.ResponseWriter, r *http.Request) {
	entity, err := h.service.Get(r.Context(), chi.URLParam(r, "entityID"))
	if errors.Is(err, repository.ErrCanonicalEntityNotFound) {
		problem(w, r, http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND", "no such canonical entity", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "canonical entity lookup failed", true)
		return
	}
	w.Header().Set("ETag", entityTag(entity.Version))
	writeJSON(w, http.StatusOK, newCanonicalEntityResponse(entity))
}

func (h canonicalHandler) lifecycle(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ifMatch := r.Header.Get("If-Match")
		if strings.TrimSpace(ifMatch) == "" {
			problem(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match with the entity's current version is required", false)
			return
		}
		version, err := service.ParseExpectedVersion(ifMatch)
		if err != nil {
			problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", err.Error(), false)
			return
		}
		id := chi.URLParam(r, "entityID")
		switch action {
		case "validate":
			_, err = h.service.Validate(r.Context(), id, version)
		case "activate":
			_, err = h.service.Activate(r.Context(), id, version)
		case "suspend":
			actor, ok := iamAuditActor(r)
			if !ok {
				problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
				return
			}
			_, err = h.service.SuspendAs(r.Context(), id, version, actor)
		case "retire":
			_, err = h.service.Retire(r.Context(), id, version)
		}
		switch {
		case err == nil:
		case errors.Is(err, repository.ErrCanonicalEntityNotFound):
			problem(w, r, http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND", "no such canonical entity", false)
			return
		case errors.Is(err, repository.ErrCanonicalEntityVersionConflict):
			problem(w, r, http.StatusPreconditionFailed, "CANONICAL_ENTITY_VERSION_MISMATCH", "the canonical entity changed after you read it; reload it before changing it", false)
			return
		case errors.Is(err, repository.ErrCanonicalEntityLifecycleConflict):
			problem(w, r, http.StatusConflict, "CANONICAL_ENTITY_LIFECYCLE_CONFLICT", err.Error(), false)
			return
		default:
			problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "canonical entity lifecycle change failed", true)
			return
		}
		entity, err := h.service.Get(r.Context(), id)
		if err != nil {
			problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "canonical entity could not be read back", true)
			return
		}
		w.Header().Set("ETag", entityTag(entity.Version))
		writeJSON(w, http.StatusOK, newCanonicalEntityResponse(entity))
	}
}

// entityTag is a version as the strong entity tag the contract's ETag and
// If-Match headers carry, e.g. "3".
func entityTag(version int64) string {
	return `"` + strconv.FormatInt(version, 10) + `"`
}
