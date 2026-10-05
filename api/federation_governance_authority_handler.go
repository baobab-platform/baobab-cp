package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const federationGovernanceAudience = "baobab-control-plane"

var federationSHA256 = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type federationGovernanceTargetPolicy struct {
	systemNamespace  string
	engineCode       string
	nativeEntityType string
}

// Ownership comes from Shared identity/v1/federation-authority-policy.yaml.
// This map says who owns the registered target. It does not make the
// ExternalReference an approval.
var federationGovernanceTargets = map[string]federationGovernanceTargetPolicy{
	"federation_configuration":    {"baobab_iam", "baobab-iam", "federation_configuration"},
	"federation_trust_material":   {"baobab_iam", "baobab-iam", "federation_trust_material"},
	"assurance_policy":             {"baobab_iam", "baobab-iam", "assurance_policy"},
	"attribute_mapping":            {"baobab_iam", "baobab-iam", "attribute_mapping"},
	"provisioning_policy":          {"baobab_iam", "baobab-iam", "provisioning_policy"},
	"federation_activation":        {"baobab_iam", "baobab-iam", "federation_activation"},
	"assurance_mapping_decision":   {"baobab_iam", "baobab-iam", "assurance_mapping_decision"},
	"canonical_identity_mapping":   {"baobab_cp", "baobab-cp", "canonical_identity_mapping"},
}

var federationApprovalPermission = map[string]string{
	"PROPOSE": "security.federation.propose",
	"DECIDE":  "security.federation.decide",
	"REVOKE":  "security.federation.revoke",
}

type federationGovernanceExpectation struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	TrustID          string `json:"trust_id"`
	SnapshotID       string `json:"snapshot_id"`
	TrustRevision    uint64 `json:"trust_revision"`
	ProviderID       string `json:"provider_id"`
	EngineInstanceID string `json:"engine_instance_id"`
	Scope            struct {
		OrganisationID string `json:"organisation_id"`
		EstateID       string `json:"estate_id"`
	} `json:"scope"`
	EventID            string `json:"event_id,omitempty"`
	Issuer             string `json:"issuer,omitempty"`
	Subject            string `json:"subject,omitempty"`
	Level              string `json:"level,omitempty"`
	EvidenceDigest     string `json:"evidence_digest,omitempty"`
	PrincipalID        string `json:"principal_id,omitempty"`
	ExternalIdentityID string `json:"external_identity_id,omitempty"`
}

type federationApprovalAuthorityRequest struct {
	Action       string                         `json:"action"`
	Target       federationGovernanceExpectation `json:"target"`
	SubjectToken string                         `json:"subject_token"`
}

type federationApprovalAuthorityResponse struct {
	PrincipalID string    `json:"principal_id"`
	ValidUntil  time.Time `json:"valid_until"`
}

func validFederationGovernanceExpectation(w federationGovernanceExpectation) (federationGovernanceTargetPolicy, bool) {
	policy, ok := federationGovernanceTargets[w.Kind]
	if !ok ||
		!domain.ValidExternalReferenceID(w.ID) ||
		!federationActorUUID.MatchString(w.TrustID) ||
		w.SnapshotID == "" || len(w.SnapshotID) > 128 || strings.TrimSpace(w.SnapshotID) != w.SnapshotID ||
		w.TrustRevision < 1 ||
		!domain.ValidProviderID(w.ProviderID) ||
		!domain.ValidEngineInstanceID(w.EngineInstanceID) ||
		len(w.Scope.OrganisationID) < 3 || len(w.Scope.OrganisationID) > 128 || !federationOrgID.MatchString(w.Scope.OrganisationID) ||
		len(w.Scope.EstateID) < 3 || len(w.Scope.EstateID) > 63 || !federationEstateID.MatchString(w.Scope.EstateID) {
		return federationGovernanceTargetPolicy{}, false
	}

	switch w.Kind {
	case "federation_configuration", "federation_trust_material", "assurance_policy", "attribute_mapping", "provisioning_policy", "federation_activation":
		return policy, w.EventID == "" && w.Issuer == "" && w.Subject == "" && w.Level == "" &&
			w.EvidenceDigest == "" && w.PrincipalID == "" && w.ExternalIdentityID == ""
	case "assurance_mapping_decision":
		return policy,
			federationActorUUID.MatchString(w.EventID) &&
				w.Issuer != "" && len(w.Issuer) <= 2048 &&
				w.Subject != "" && len(w.Subject) <= 255 &&
				(w.Level == "BAOBAB-A1" || w.Level == "BAOBAB-A2" || w.Level == "BAOBAB-A3") &&
				federationSHA256.MatchString(w.EvidenceDigest) &&
				w.PrincipalID == "" && w.ExternalIdentityID == ""
	case "canonical_identity_mapping":
		return policy,
			w.Issuer != "" && len(w.Issuer) <= 2048 &&
				w.Subject != "" && len(w.Subject) <= 255 &&
				federationActorUUID.MatchString(w.PrincipalID) &&
				federationActorUUID.MatchString(w.ExternalIdentityID) &&
				w.EventID == "" && w.Level == "" && w.EvidenceDigest == ""
	default:
		return federationGovernanceTargetPolicy{}, false
	}
}

// approvalAuthority is CP's human maker/checker authority source.
//
// Two independently verified parties are mandatory:
//   - the request bearer authenticates the IAM workload and federation-authority:read;
//   - subject_token authenticates the human whose current AdministrativeGrant
//     is being evaluated.
//
// CP does not return IAM roles, emails or the IAM service principal as actor.
// It also does not claim that a registered ExternalReference is approved.
func (h federationAuthorityHandler) approvalAuthority(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	workload, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !h.callerActive(workload) {
		h.deny(w, http.StatusForbidden)
		return
	}
	if h.api == nil || h.api.grants == nil || h.caller == nil || h.targets == nil || h.subjects == nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}

	var req federationApprovalAuthorityRequest
	if !readFederationAuthorityBody(w, r, &req) ||
		len(req.SubjectToken) == 0 || len(req.SubjectToken) > 16384 ||
		strings.ContainsAny(req.SubjectToken, " \t\r\n") {
		h.deny(w, http.StatusBadRequest)
		return
	}
	permission, ok := federationApprovalPermission[req.Action]
	if !ok {
		h.deny(w, http.StatusBadRequest)
		return
	}
	targetPolicy, ok := validFederationGovernanceExpectation(req.Target)
	if !ok {
		h.deny(w, http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	query := repository.FederationGovernanceTargetQuery{
		ReferenceID:      req.Target.ID,
		Kind:             req.Target.Kind,
		SystemNamespace:  targetPolicy.systemNamespace,
		EngineCode:       targetPolicy.engineCode,
		NativeEntityType: targetPolicy.nativeEntityType,
		ProviderID:       req.Target.ProviderID,
		EngineInstanceID: req.Target.EngineInstanceID,
		OrganisationID:   req.Target.Scope.OrganisationID,
		DigitalEstateID:  req.Target.Scope.EstateID,
		Environment:      h.api.environment,
	}
	registration, err := h.targets.ReadFederationGovernanceTargetRegistration(r.Context(), query, now)
	switch {
	case errors.Is(err, repository.ErrFederationGovernanceTargetNotFound):
		h.deny(w, http.StatusConflict)
		return
	case err != nil:
		h.deny(w, http.StatusServiceUnavailable)
		return
	}

	verifier, err := h.subjects.For(r.Context(), federationGovernanceAudience)
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}
	human, err := verifier.Verify(r.Context(), req.SubjectToken)
	if err != nil ||
		human.ActorType != "human" ||
		!human.HasScope("federation-governance:manage") ||
		human.Issuer == "" || human.Subject == "" ||
		human.ExpiresAt.IsZero() || !now.Before(human.ExpiresAt) {
		h.deny(w, http.StatusForbidden)
		return
	}

	identity, err := h.caller.ReadFederationIdentity(r.Context(), human.Issuer, human.Subject)
	if err != nil {
		if errors.Is(err, repository.ErrIdentityNotFound) {
			h.deny(w, http.StatusForbidden)
		} else {
			h.deny(w, http.StatusServiceUnavailable)
		}
		return
	}
	if !federationActorUUID.MatchString(identity.Principal.ID) ||
		!federationActorUUID.MatchString(identity.ExternalIdentity.ID) ||
		identity.Principal.Status != "ACTIVE" ||
		identity.ExternalIdentity.Status != "ACTIVE" ||
		identity.Principal.ActorType != "human" ||
		identity.ExternalIdentity.Issuer != human.Issuer ||
		identity.ExternalIdentity.Subject != human.Subject ||
		identity.ExternalIdentity.PrincipalID != identity.Principal.ID {
		h.deny(w, http.StatusForbidden)
		return
	}

	grants, sources, err := h.api.grants.AdministrativeGrantsOf(r.Context(), identity.Principal.ID)
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}
	relations, err := h.api.grants.EffectiveRelations(r.Context(), []string{registration.TenantID}, now)
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}
	resource := relations.ResolveResource(administration.Resource{
		Environment:     h.api.environment,
		TenantID:        registration.TenantID,
		OrganisationID:  req.Target.Scope.OrganisationID,
		DigitalEstateID: req.Target.Scope.EstateID,
		ResourceType:    req.Target.Kind,
		ResourceID:      req.Target.ID,
	})
	session := administration.Session{
		ACR:             human.Assurance.ACR,
		AMR:             human.Assurance.AMR,
		AuthenticatedAt: human.Assurance.AuthenticatedAt,
	}
	decision := administration.Evaluate(administration.Request{
		PrincipalID:     identity.Principal.ID,
		PrincipalActive: true,
		Action:          permission,
		Resource:        resource,
		Relations:       relations,
		Now:             now,
		Session:         session,
		Grants:          grants,
		Sources:         sources,
	})
	if !decision.Allowed() {
		h.deny(w, http.StatusForbidden)
		return
	}
	validUntil, ok := federationApprovalValidUntil(now, human, decision, grants, sources)
	if !ok {
		h.deny(w, http.StatusForbidden)
		return
	}

	// Re-read the CP-owned registration/topology evidence after authorization.
	// If it moved while the human authority was evaluated, the caller must
	// restart against a coherent target instead of receiving a mixed decision.
	current, err := h.targets.ReadFederationGovernanceTargetRegistration(r.Context(), query, time.Now().UTC())
	if errors.Is(err, repository.ErrFederationGovernanceTargetNotFound) ||
		(err == nil && current != registration) {
		h.deny(w, http.StatusConflict)
		return
	}
	if err != nil {
		h.deny(w, http.StatusServiceUnavailable)
		return
	}
	if !h.callerActive(workload) {
		h.deny(w, http.StatusForbidden)
		return
	}

	writeJSON(w, http.StatusOK, federationApprovalAuthorityResponse{
		PrincipalID: identity.Principal.ID,
		ValidUntil:  validUntil,
	})
}

func federationApprovalValidUntil(
	now time.Time,
	principal auth.Principal,
	decision administration.Decision,
	grants []administration.Grant,
	sources map[string]administration.Grant,
) (time.Time, bool) {
	if principal.ExpiresAt.IsZero() || !now.Before(principal.ExpiresAt) || len(decision.MatchedGrants) == 0 {
		return time.Time{}, false
	}
	validUntil := principal.ExpiresAt.UTC()
	byID := make(map[string]administration.Grant, len(grants))
	for _, grant := range grants {
		byID[grant.GrantID] = grant
	}
	policy := administration.MustDefaultAssurance()
	for _, id := range decision.MatchedGrants {
		grant, ok := byID[id]
		if !ok {
			return time.Time{}, false
		}
		if grant.ValidUntil != nil && grant.ValidUntil.Before(validUntil) {
			validUntil = grant.ValidUntil.UTC()
		}
		requirement := policy.Required(grant)
		if requirement.MaxAuthenticationAge > 0 {
			if principal.Assurance.AuthenticatedAt.IsZero() {
				return time.Time{}, false
			}
			freshUntil := principal.Assurance.AuthenticatedAt.UTC().Add(time.Duration(requirement.MaxAuthenticationAge) * time.Second)
			if freshUntil.Before(validUntil) {
				validUntil = freshUntil
			}
		}

		current := grant
		for hops := 0; current.Source == administration.SourceDelegation; hops++ {
			if hops >= 3 {
				return time.Time{}, false
			}
			parent, ok := sources[current.DelegatedFromGrantID]
			if !ok {
				return time.Time{}, false
			}
			if parent.ValidUntil != nil && parent.ValidUntil.Before(validUntil) {
				validUntil = parent.ValidUntil.UTC()
			}
			current = parent
		}
	}
	return validUntil, now.Before(validUntil)
}
