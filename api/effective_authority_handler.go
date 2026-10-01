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
	// An inactive principal holds no usable authority (ADR-BCP-020 section
	// 94): it is refused rather than shown grants it cannot use.
	caller, err := h.identities.GetPrincipal(r.Context(), principalID)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "the caller's identity could not be resolved", true)
		return
	}
	if caller.Status != "ACTIVE" {
		problem(w, r, http.StatusForbidden, "PRINCIPAL_INACTIVE", "the caller's Control Plane principal is not active", false)
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
	// A delegation across levels (an organisation grant delegated at one of
	// its tenants) is usable only while the mapping it rests on is effective.
	scopes := make([]administration.Scope, 0, len(grants)+len(sources))
	for _, g := range grants {
		scopes = append(scopes, g.Scope)
	}
	for _, g := range sources {
		scopes = append(scopes, g.Scope)
	}
	rel, err := h.grants.EffectiveRelations(r.Context(), administration.TenantsOf(scopes...), now)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "effective authority could not be read", true)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, administration.Effective(principalID, grants, sources, now, rel))
}
