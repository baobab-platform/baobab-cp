package api

import (
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// effectiveAuthorityHandler serves GET /v1/admin/effective-authority: the
// caller's own ACTIVE, currently valid AdministrativeGrants (ADR-BCP-020
// sections 99-102, 110; Shared administration/v1 EffectiveAuthority). It is
// derived from grants only, never from IAM roles or memberships, and
// authorises nothing: every administrative request is evaluated again.
type effectiveAuthorityHandler struct {
	identities repository.IdentityRepository
	grants     repository.AdministrativeGrantReader
	now        func() time.Time
}

func (h effectiveAuthorityHandler) get(w http.ResponseWriter, r *http.Request) {
	// Platform staff must already have a Control Plane principal; reading
	// one's authority never provisions one.
	principalID, _, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	grants, sources, err := h.grants.AdministrativeGrantsOf(r.Context(), principalID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "effective authority could not be read", true)
		return
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now()
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, administration.Effective(principalID, grants, sources, now))
}
