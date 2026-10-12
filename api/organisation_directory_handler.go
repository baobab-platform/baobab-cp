package api

// CX-04B: only the caller's currently effective, EXACT organisation.view
// grants can produce directory rows. No inferred child, platform or group
// authority is ever expanded.

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type organisationDirectoryEntry struct {
	OrganisationID    string                   `json:"organisation_id"`
	DisplayName       string                   `json:"display_name"`
	Status            string                   `json:"status"`
	VerificationState domain.VerificationState `json:"verification_state"`
	SourceAuthority   string                   `json:"source_authority"`
	Jurisdiction      string                   `json:"jurisdiction,omitempty"`
}
type organisationDirectoryResponse struct {
	EvaluatedAt time.Time                    `json:"evaluated_at"`
	Items       []organisationDirectoryEntry `json:"items"`
}

// exactDirectoryIDs is a display listing boundary, not an authorisation
// decision for subsequent operations. Every detail request checks again.
func exactDirectoryIDs(authority administration.EffectiveAuthority, environment string) ([]string, bool) {
	out := []string{}
	seen := map[string]bool{}
	for _, grant := range authority.Grants {
		scope := grant.Scope
		if grant.Permission != "organisation.view" || scope.Level != administration.LevelOrganisation ||
			(scope.Mode != "" && scope.Mode != administration.ModeExact) ||
			(scope.Environment != "" && scope.Environment != environment) ||
			!domain.IsUUID(scope.OrganisationID) {
			continue
		}
		id := strings.ToLower(scope.OrganisationID)
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(seen) > maxOverviewRows {
			return nil, false
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, true
}

func (h organisationOverviewHandler) directory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if len(r.URL.Query()) != 0 {
		problem(w, r, http.StatusBadRequest, "INVALID_DIRECTORY_REQUEST", "directory has no untrusted filters", false)
		return
	}
	if h.reader == nil || h.grants == nil || h.identities == nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_DIRECTORY_UNAVAILABLE", "authoritative directory unavailable", true)
		return
	}
	actor, _, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	principal, err := h.identities.GetPrincipal(r.Context(), actor)
	if err != nil || principal.Status != "ACTIVE" {
		problem(w, r, http.StatusForbidden, "DIRECTORY_DENIED", "caller identity unavailable", false)
		return
	}
	grants, sources, err := h.grants.AdministrativeGrantsOf(r.Context(), actor)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "DIRECTORY_AUTHORITY_UNAVAILABLE", "effective grants unavailable", true)
		return
	}
	at := time.Now().UTC()
	if h.now != nil {
		at = h.now()
	}
	scopes := make([]administration.Scope, 0, len(grants)+len(sources))
	for _, grant := range grants {
		scopes = append(scopes, grant.Scope)
	}
	for _, grant := range sources {
		scopes = append(scopes, grant.Scope)
	}
	relations, err := h.grants.EffectiveRelations(r.Context(), administration.TenantsOf(scopes...), at)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "DIRECTORY_AUTHORITY_UNAVAILABLE", "effective relations unavailable", true)
		return
	}
	response := organisationDirectoryResponse{EvaluatedAt: at, Items: []organisationDirectoryEntry{}}
	ids, valid := exactDirectoryIDs(administration.Effective(actor, grants, sources, at, relations), h.environment)
	if !valid {
		problem(w, r, http.StatusServiceUnavailable, "DIRECTORY_INCOMPLETE", "directory exceeds bounded limit", true)
		return
	}
	for _, id := range ids {
		profile, err := h.reader.GetOrganisation(r.Context(), id)
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "DIRECTORY_UNAVAILABLE", "organisation registry read failed", true)
			return
		}
		if profile == nil {
			continue
		}
		if !strings.EqualFold(profile.CanonicalEntityID, id) {
			problem(w, r, http.StatusServiceUnavailable, "DIRECTORY_INCONSISTENT", "organisation source mismatch", true)
			return
		}
		response.Items = append(response.Items, organisationDirectoryEntry{
			OrganisationID: id, DisplayName: profile.DisplayName, Status: profile.Status,
			VerificationState: profile.VerificationState, SourceAuthority: profile.SourceAuthority,
			Jurisdiction: profile.Jurisdiction,
		})
	}
	sort.Slice(response.Items, func(i, j int) bool {
		if response.Items[i].DisplayName == response.Items[j].DisplayName {
			return response.Items[i].OrganisationID < response.Items[j].OrganisationID
		}
		return response.Items[i].DisplayName < response.Items[j].DisplayName
	})
	writeJSON(w, http.StatusOK, response)
}
