// Target path: api/external_reference_handler.go
//
// Gate ZB-03.3: the ExternalReference persistence layer that ADR-BCP-016
// registered a type for but nothing implemented (docs/reconciliation/
// gate-zb03-authority-contract-freeze.md §2 -- "no downstream engine
// identifier SHALL become the universal Baobab counterparty identifier",
// ADR-BCP-014 §18-19 -- "Canonical Organisation -> ExternalReference ->
// Keycloak Organization" is the only permitted path). This is the
// onboarding-time link between a CanonicalEntity (e.g. a BUYER_ORGANISATION
// registered under ADR-BCP-016) and its identity-side counterpart in
// another system -- never consulted by a runtime authorization decision,
// which always verifies a caller-asserted organisation_id names a
// CanonicalEntity directly (AuthoritativeContextResolver.Resolve's
// OrganisationID stage, internal/provisioning/context_resolver.go).
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

type externalReferenceHandler struct {
	repo repository.ExternalReferenceRepository
}

type createExternalReferenceRequest struct {
	EngineID    string `json:"engine_id"`
	NativeType  string `json:"native_type"`
	NativeID    string `json:"native_id"`
	ExternalURL string `json:"external_url,omitempty"`
}

func (h externalReferenceHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createExternalReferenceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not valid external reference JSON", false)
		return
	}
	ref := domain.ExternalReference{
		// Same BCP-DB-001/BCP-GO-001 convention canonicalHandler.create
		// follows: Go mints the UUIDv7, never the database.
		ID:                domain.NewUUIDv7(),
		CanonicalEntityID: chi.URLParam(r, "entityID"),
		EngineID:          req.EngineID,
		NativeType:        req.NativeType,
		NativeID:          req.NativeID,
		ExternalURL:       req.ExternalURL,
	}
	created, err := h.repo.CreateExternalReference(r.Context(), ref)
	switch {
	case err == nil:
		w.Header().Set("Location", "/v1/canonical-entities/"+ref.CanonicalEntityID+"/external-references/"+created.ID)
		writeJSON(w, http.StatusCreated, created)
	case errors.Is(err, repository.ErrExternalReferenceAlreadyLinked):
		problem(w, r, http.StatusConflict, "EXTERNAL_REFERENCE_ALREADY_LINKED", err.Error(), false)
	case errors.Is(err, repository.ErrCanonicalEntityNotFound):
		problem(w, r, http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND", err.Error(), false)
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	}
}

type lookupExternalReferenceResponse struct {
	CanonicalEntity domain.CanonicalEntity `json:"canonical_entity"`
}

// lookup resolves a native-system identifier (e.g. a Keycloak Organization
// ID) back to the CanonicalEntity it was linked to -- the reverse direction
// of create, used by onboarding/backfill flows that need to populate a
// downstream engine's own canonical_organisation_id-shaped column (e.g.
// baobab-trade's b2b_organisation.canonical_organisation_id).
func (h externalReferenceHandler) lookup(w http.ResponseWriter, r *http.Request) {
	engineID := r.URL.Query().Get("engine_id")
	nativeType := r.URL.Query().Get("native_type")
	nativeID := r.URL.Query().Get("native_id")
	if engineID == "" || nativeType == "" || nativeID == "" {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "engine_id, native_type and native_id query parameters are required", false)
		return
	}
	entity, err := h.repo.GetCanonicalEntityByExternalReference(r.Context(), engineID, nativeType, nativeID)
	if errors.Is(err, repository.ErrExternalReferenceAmbiguous) {
		problem(w, r, http.StatusConflict, "EXTERNAL_REFERENCE_AMBIGUOUS", "that external reference is linked to more than one canonical entity", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusNotFound, "EXTERNAL_REFERENCE_NOT_FOUND", "no canonical entity is linked to that external reference", false)
		return
	}
	writeJSON(w, http.StatusOK, lookupExternalReferenceResponse{CanonicalEntity: entity})
}
