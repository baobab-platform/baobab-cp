package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
		"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

var (
	federationActorUUID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
	federationOrgID     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
	federationEstateID  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
)

type federationAuthorityHandler struct {
	api       *API
	caller    repository.FederationIdentityReader
	canonical *service.FederationIdentityEvidenceService
	platform  repository.IdentityRuntimeProfileRepository
}

func (h federationAuthorityHandler) deny(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":"authority request denied"}`)
}

func (h federationAuthorityHandler) callerActive(p auth.Principal) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" || p.ClientID == "" ||
		!p.HasScope("federation-authority:read") || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	if scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry); ok &&
		!scopes.AllowsScope(p.ClientID, "federation-authority:read") {
		return false
	}
	return true
}

func readFederationAuthorityBody(w http.ResponseWriter, r *http.Request, into any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.URL.RawQuery != "" {
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil || rejectDuplicateJSON(raw) != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return false
	}
	var trailing any
	return decoder.Decode(&trailing) == io.EOF
}

// identity is a private authority source, never login or identity provisioning.
// It preserves the existing platform-authority requirement because issuer+subject
// itself carries no organisation or estate scope.
func (h federationAuthorityHandler) identity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || h.api == nil || h.canonical == nil || !h.callerActive(p) {
		h.deny(w, http.StatusForbidden)
		return
	}

	var req struct {
		Issuer  string
		Subject string
	}
	if !readFederationAuthorityBody(w, r, &req) || req.Issuer == "" || req.Subject == "" {
		h.deny(w, http.StatusBadRequest)
		return
	}

	authorize := func(reference string) (string, bool) {
		identity, err := h.caller.ReadFederationIdentity(r.Context(), p.Issuer, p.Subject)
		if err != nil || r.Context().Err() != nil ||
			!federationActorUUID.MatchString(identity.Principal.ID) ||
			!federationActorUUID.MatchString(identity.ExternalIdentity.ID) ||
			identity.Principal.Status != "ACTIVE" ||
			identity.ExternalIdentity.Status != "ACTIVE" ||
			identity.Principal.ActorType != "workload" ||
			identity.ExternalIdentity.Issuer != p.Issuer ||
			identity.ExternalIdentity.Subject != p.Subject ||
			identity.ExternalIdentity.PrincipalID != identity.Principal.ID {
			return "", false
		}
		grants, sources, err := h.api.grants.AdministrativeGrantsOf(r.Context(), identity.Principal.ID)
		if err != nil {
			return "", false
		}
		decision := administration.Evaluate(administration.Request{
			PrincipalID:     identity.Principal.ID,
			PrincipalActive: true,
			Action:          "security.federation.view",
			Resource: administration.Resource{
				Environment:  h.api.environment,
				ResourceType: "canonical_identity_mapping",
				ResourceID:   reference,
			},
			Now: time.Now().UTC(),
			Session: administration.Session{
				ACR:             p.Assurance.ACR,
				AMR:             p.Assurance.AMR,
				AuthenticatedAt: p.Assurance.AuthenticatedAt,
			},
			Grants:  grants,
			Sources: sources,
		})
		return identity.Principal.ID, decision.Allowed()
	}

	actor, allowed := authorize("")
	if !allowed {
		h.deny(w, http.StatusForbidden)
		return
	}
	out, err := h.canonical.Resolve(r.Context(), req.Issuer, req.Subject)
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}
	current, allowed := authorize(out.MappingReference)
	if !allowed || current != actor || !h.callerActive(p) {
		h.deny(w, http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type federationBindingRequest struct {
	Binding struct {
		ProviderID             string `json:"provider_id"`
		EngineInstanceID       string `json:"engine_instance_id"`
		ConfigurationReference string `json:"configuration_reference"`
		TrustMaterialReference string `json:"trust_material_reference"`
	}
	Scope struct {
		OrganisationID string
		EstateID       string
	}
	RuntimeCapability string
}

// binding is CP's live PlatformAuthority source. It answers only when a real
// ACTIVE identity.authentication.perform binding and fresh runtime/deployment
// evidence agree for the exact provider, instance, organisation, estate,
// federation facet and trust configuration reference.
func (h federationAuthorityHandler) binding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || h.api == nil || h.platform == nil || !h.callerActive(p) {
		h.deny(w, http.StatusForbidden)
		return
	}

	var req federationBindingRequest
	if !readFederationAuthorityBody(w, r, &req) ||
		!domain.ValidProviderID(req.Binding.ProviderID) ||
		!domain.ValidEngineInstanceID(req.Binding.EngineInstanceID) ||
		!domain.ValidExternalReferenceID(req.Binding.ConfigurationReference) ||
		!domain.ValidExternalReferenceID(req.Binding.TrustMaterialReference) ||
		len(req.Scope.OrganisationID) < 3 || len(req.Scope.OrganisationID) > 128 || !federationOrgID.MatchString(req.Scope.OrganisationID) ||
		len(req.Scope.EstateID) < 3 || len(req.Scope.EstateID) > 63 || !federationEstateID.MatchString(req.Scope.EstateID) ||
		(req.RuntimeCapability != "OIDC_FEDERATION" && req.RuntimeCapability != "SAML_FEDERATION") {
		h.deny(w, http.StatusBadRequest)
		return
	}

	resource := administration.Resource{
		Environment:     h.api.environment,
		OrganisationID:  req.Scope.OrganisationID,
		DigitalEstateID: req.Scope.EstateID,
		ResourceType:    "identity_runtime_profile",
		ResourceID:      req.Binding.ProviderID,
	}
	actor, allowed := h.authorizeRequest(r, p, resource)
	if !allowed {
		h.deny(w, http.StatusForbidden)
		return
	}

	out, err := h.platform.ReadFederationPlatformSnapshot(
		r.Context(),
		req.Binding.ProviderID,
		req.Binding.EngineInstanceID,
		req.Scope.OrganisationID,
		req.Scope.EstateID,
		req.RuntimeCapability,
		req.Binding.ConfigurationReference,
		h.api.environment,
		time.Now().UTC(),
	)
	if errors.Is(err, repository.ErrFederationPlatformEvidenceNotFound) {
		h.deny(w, http.StatusConflict)
		return
	}
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}

	runtimeObservers, runtimeOK := h.api.workloadRegistry.(auth.IdentityRuntimeObserverRegistry)
	deploymentReporters, deploymentOK := h.api.workloadRegistry.(auth.ReporterRegistry)
	if !runtimeOK || !deploymentOK ||
		!currentRuntimeEvidenceSource(runtimeObservers, out.RuntimeEvidenceSource, out.EvidenceEnvironment, out.EvidenceRegion) ||
		!currentDeploymentEvidenceSource(deploymentReporters, out.DeploymentEvidenceSource, out.EvidenceEnvironment, out.EvidenceRegion) {
		h.deny(w, http.StatusConflict)
		return
	}

	current, allowed := h.authorizeRequest(r, p, resource)
	if !allowed || current != actor || !h.callerActive(p) {
		h.deny(w, http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h federationAuthorityHandler) authorizeRequest(r *http.Request, p auth.Principal, resource administration.Resource) (string, bool) {
	if !h.callerActive(p) || h.caller == nil || h.api == nil || h.api.grants == nil {
		return "", false
	}
	identity, err := h.caller.ReadFederationIdentity(r.Context(), p.Issuer, p.Subject)
	if err != nil || r.Context().Err() != nil ||
		!federationActorUUID.MatchString(identity.Principal.ID) ||
		!federationActorUUID.MatchString(identity.ExternalIdentity.ID) ||
		identity.Principal.Status != "ACTIVE" ||
		identity.ExternalIdentity.Status != "ACTIVE" ||
		identity.Principal.ActorType != "workload" ||
		identity.ExternalIdentity.Issuer != p.Issuer ||
		identity.ExternalIdentity.Subject != p.Subject ||
		identity.ExternalIdentity.PrincipalID != identity.Principal.ID {
		return "", false
	}
	grants, sources, err := h.api.grants.AdministrativeGrantsOf(r.Context(), identity.Principal.ID)
	if err != nil {
		return "", false
	}
	decision := administration.Evaluate(administration.Request{
		PrincipalID:     identity.Principal.ID,
		PrincipalActive: true,
		Action:          "security.federation.view",
		Resource:        resource,
		Now:             time.Now().UTC(),
		Session: administration.Session{
			ACR:             p.Assurance.ACR,
			AMR:             p.Assurance.AMR,
			AuthenticatedAt: p.Assurance.AuthenticatedAt,
		},
		Grants:  grants,
		Sources: sources,
	})
	return identity.Principal.ID, decision.Allowed()
}

func currentRuntimeEvidenceSource(registry auth.IdentityRuntimeObserverRegistry, source, environment, region string) bool {
	clientID, ok := strings.CutPrefix(source, "workload:")
	if !ok || clientID == "" {
		return false
	}
	scope, ok := registry.IdentityRuntimeObserver(clientID)
	return ok && scope.Allows(environment, region)
}

func currentDeploymentEvidenceSource(registry auth.ReporterRegistry, source, environment, region string) bool {
	clientID, ok := strings.CutPrefix(source, "workload:")
	if !ok || clientID == "" {
		return false
	}
	scope, ok := registry.Reporter(clientID)
	return ok && scope.Allows(environment, region)
}

