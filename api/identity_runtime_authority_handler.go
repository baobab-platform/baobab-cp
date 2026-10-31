package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// Separate from federation: this endpoint cannot approve configuration or
// issue tokens. CP evaluates a scoped read grant around each repository read.
type identityRuntimeAuthorityHandler struct {
	federationAuthorityHandler
	runtime repository.IdentityRuntimeAuthorityReader
}

type identityRuntimeBindingRequest struct {
	ProviderID              string `json:"provider_id"`
	EngineInstanceID        string `json:"engine_instance_id"`
	OrganisationID          string `json:"organisation_id"`
	EstateID                string `json:"estate_id"`
	RuntimeCapability       string `json:"runtime_capability"`
	ConfigurationReference  string `json:"configuration_reference"`
	SecurityDomainReference string `json:"security_domain_reference"`
}

// identityRuntimeSnapshotWire is the versioned private runtime response.
// Keep explicit tags: internal Go model names are not an HTTP wire contract.
type identityRuntimeSnapshotWire struct {
	ProviderID       string `json:"provider_id"`
	EngineInstanceID string `json:"engine_instance_id"`
	Scope            struct {
		OrganisationID string `json:"organisation_id"`
		EstateID       string `json:"estate_id"`
	} `json:"scope"`
	ProviderStatus          string    `json:"provider_status"`
	InstanceStatus          string    `json:"instance_status"`
	BindingStatus           string    `json:"binding_status"`
	RuntimeCapability       string    `json:"runtime_capability"`
	SupportStatus           string    `json:"support_status"`
	ArtifactDigest          string    `json:"artifact_digest"`
	DeployedArtifactDigest  string    `json:"deployed_artifact_digest"`
	ProfileRevision         uint64    `json:"profile_revision"`
	EvidenceExpiresAt       time.Time `json:"evidence_expires_at"`
	ConfigurationReference  string    `json:"configuration_reference"`
	SecurityDomainReference string    `json:"security_domain_reference"`
}

func runtimeSnapshotWire(out repository.FederationPlatformSnapshot, req identityRuntimeBindingRequest) identityRuntimeSnapshotWire {
	wire := identityRuntimeSnapshotWire{
		ProviderID: out.ProviderID, EngineInstanceID: out.EngineInstanceID,
		ProviderStatus: out.ProviderStatus, InstanceStatus: out.InstanceStatus, BindingStatus: out.BindingStatus,
		RuntimeCapability: out.RuntimeCapability, SupportStatus: out.SupportStatus,
		ArtifactDigest: out.ArtifactDigest, DeployedArtifactDigest: out.DeployedArtifactDigest,
		ProfileRevision: out.ProfileRevision, EvidenceExpiresAt: out.EvidenceExpiresAt,
		ConfigurationReference: req.ConfigurationReference, SecurityDomainReference: req.SecurityDomainReference,
	}
	wire.Scope.OrganisationID, wire.Scope.EstateID = out.Scope.OrganisationID, out.Scope.EstateID
	return wire
}

// exactN04RuntimeReadback performs a final boundary check on CP's returned
// private runtime binding. It does not infer authority: the repository MUST
// still prove current lifecycle, approved release/certification and valid
// evidence in its PostgreSQL transaction. A substituted or stale-shaped
// projection must never be released to an IAM or resource-side reader.
func exactN04RuntimeReadback(req identityRuntimeBindingRequest,
	out repository.FederationPlatformSnapshot) bool {
	return out.ProviderID == req.ProviderID &&
		out.EngineInstanceID == req.EngineInstanceID &&
		out.Scope.OrganisationID == req.OrganisationID &&
		out.Scope.EstateID == req.EstateID &&
		out.RuntimeCapability == req.RuntimeCapability &&
		out.ProviderStatus == "ACTIVE" &&
		out.InstanceStatus == "ACTIVE" &&
		out.BindingStatus == "ACTIVE" &&
		out.SupportStatus == "VERIFIED" &&
		out.ProfileRevision > 0 &&
		out.ArtifactDigest != "" &&
		out.ArtifactDigest == out.DeployedArtifactDigest &&
		!out.EvidenceExpiresAt.IsZero()
}

func (h identityRuntimeAuthorityHandler) active(p auth.Principal) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" ||
		p.ClientID == "" || !p.HasScope("identity-runtime:read") || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	return ok && scopes.AllowsScope(p.ClientID, "identity-runtime:read")
}

func (h identityRuntimeAuthorityHandler) binding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || h.runtime == nil || !h.active(p) {
		h.deny(w, http.StatusForbidden)
		return
	}
	var req identityRuntimeBindingRequest
	if !readFederationAuthorityBody(w, r, &req) ||
		!domain.ValidProviderID(req.ProviderID) || !domain.ValidEngineInstanceID(req.EngineInstanceID) ||
		!domain.ValidExternalReferenceID(req.ConfigurationReference) || !domain.ValidExternalReferenceID(req.SecurityDomainReference) ||
		len(req.OrganisationID) < 3 || len(req.OrganisationID) > 128 || !federationOrgID.MatchString(req.OrganisationID) ||
		len(req.EstateID) < 3 || len(req.EstateID) > 63 || !federationEstateID.MatchString(req.EstateID) ||
		repository.IdentityRuntimePlatformCapability(req.RuntimeCapability) == "" {
		h.deny(w, http.StatusBadRequest)
		return
	}
	resource := administration.Resource{Environment: h.api.environment,
		OrganisationID: req.OrganisationID, DigitalEstateID: req.EstateID,
		ResourceType: "identity_runtime_profile", ResourceID: req.ProviderID}
	actor, allowed := h.authorizeActionRequest(r, p, resource, "security.identity-runtime.view", h.active)
	if !allowed {
		h.deny(w, http.StatusForbidden)
		return
	}
	out, err := h.runtime.ReadIdentityRuntimeSnapshot(r.Context(), req.ProviderID, req.EngineInstanceID,
		req.OrganisationID, req.EstateID, req.RuntimeCapability, req.ConfigurationReference,
		req.SecurityDomainReference, h.api.environment, time.Now().UTC())
	if errors.Is(err, repository.ErrFederationPlatformEvidenceNotFound) {
		h.deny(w, http.StatusConflict)
		return
	}
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}
	if !exactN04RuntimeReadback(req, out) {
		h.deny(w, http.StatusConflict)
		return
	}
	observers, runtimeOK := h.api.workloadRegistry.(auth.IdentityRuntimeObserverRegistry)
	reporters, deploymentOK := h.api.workloadRegistry.(auth.ReporterRegistry)
	if !runtimeOK || !deploymentOK ||
		!currentRuntimeEvidenceSource(observers, out.RuntimeEvidenceSource, out.EvidenceEnvironment, out.EvidenceRegion) ||
		!currentDeploymentEvidenceSource(reporters, out.DeploymentEvidenceSource, out.EvidenceEnvironment, out.EvidenceRegion) {
		h.deny(w, http.StatusConflict)
		return
	}
	current, allowed := h.authorizeActionRequest(r, p, resource, "security.identity-runtime.view", h.active)
	if !allowed || actor != current || !h.active(p) {
		h.deny(w, http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, runtimeSnapshotWire(out, req))
}
