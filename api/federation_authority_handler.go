package api

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

var federationActorUUID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

type federationAuthorityHandler struct {
	api       *API
	caller    repository.FederationIdentityReader
	canonical *service.FederationIdentityEvidenceService
}

// identity is a private authority source, never login or identity provisioning.
// A valid workload token/scope is necessary but insufficient: current canonical
// principal/mapping, ACTIVE workload and scoped canonical grants are required.
// It deliberately bypasses legacy-role/display/enforcement-rollback fallbacks.
func (h federationAuthorityHandler) identity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	deny := func(status int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, `{"error":"authority request denied"}`)
	}
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || h.api.workloadRegistry == nil || h.caller == nil || h.api.grants == nil || h.canonical == nil || !h.api.workloadRegistry.IsActive(p.ClientID) || p.ActorType != "workload" || !p.HasScope("federation-authority:read") {
		deny(http.StatusForbidden)
		return
	}
	authorize := func(reference string) (string, bool) {
		identity, err := h.caller.ReadFederationIdentity(r.Context(), p.Issuer, p.Subject)
		if err != nil || r.Context().Err() != nil || !federationActorUUID.MatchString(identity.Principal.ID) || !federationActorUUID.MatchString(identity.ExternalIdentity.ID) || identity.Principal.Status != "ACTIVE" || identity.ExternalIdentity.Status != "ACTIVE" || identity.Principal.ActorType != "workload" || identity.ExternalIdentity.Issuer != p.Issuer || identity.ExternalIdentity.Subject != p.Subject || identity.ExternalIdentity.PrincipalID != identity.Principal.ID {
			return "", false
		}
		grants, sources, err := h.api.grants.AdministrativeGrantsOf(r.Context(), identity.Principal.ID)
		if err != nil {
			return "", false
		}
		// No organisation/estate is inferred from a human's issuer/subject. This
		// privileged reader therefore needs explicit platform authority. Narrow
		// organisation readers require a future scope-bearing source contract.
		decision := administration.Evaluate(administration.Request{PrincipalID: identity.Principal.ID, PrincipalActive: true, Action: "security.federation.view", Resource: administration.Resource{Environment: h.api.environment, ResourceType: "canonical_identity_mapping", ResourceID: reference}, Now: time.Now().UTC(), Session: administration.Session{ACR: p.Assurance.ACR, AMR: p.Assurance.AMR, AuthenticatedAt: p.Assurance.AuthenticatedAt}, Grants: grants, Sources: sources})
		return identity.Principal.ID, decision.Allowed()
	}
	actor, ok := authorize("")
	if !ok {
		deny(http.StatusForbidden)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.URL.RawQuery != "" {
		deny(http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		deny(http.StatusBadRequest)
		return
	}
	fields := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		name, valid := key.(string)
		if err != nil || !valid || name != "Issuer" && name != "Subject" {
			deny(http.StatusBadRequest)
			return
		}
		if _, exists := fields[name]; exists {
			deny(http.StatusBadRequest)
			return
		}
		var value string
		if decoder.Decode(&value) != nil || value == "" {
			deny(http.StatusBadRequest)
			return
		}
		fields[name] = value
	}
	if last, err := decoder.Token(); err != nil || last != json.Delim('}') || len(fields) != 2 {
		deny(http.StatusBadRequest)
		return
	}
	if _, err := decoder.Token(); err != io.EOF {
		deny(http.StatusBadRequest)
		return
	}
	out, err := h.canonical.Resolve(r.Context(), fields["Issuer"], fields["Subject"])
	if err != nil {
		deny(http.StatusServiceUnavailable)
		return
	}
	current, ok := authorize(out.MappingReference)
	if !ok || current != actor || !h.api.workloadRegistry.IsActive(p.ClientID) {
		deny(http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
