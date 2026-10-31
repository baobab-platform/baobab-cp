package api

import (
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// IAM N04 never accepts an unbound provider release or a foreign scoped
// PostgreSQL projection merely because a repository method returned nil.
func TestN04NativeWorkloadRuntimeResponseCannotSwapRegisteredAuthority(t *testing.T) {
	request := identityRuntimeBindingRequest{
		ProviderID: "provider_aaaabbbb",
		EngineInstanceID: "ei_aaaabbbb",
		OrganisationID: "org_development",
		EstateID: "estate_nabhold",
		RuntimeCapability: "WORKLOAD_TOKEN_ISSUANCE",
		ConfigurationReference: "ref_config1234",
		SecurityDomainReference: "ref_security1234",
	}
	sha := "sha256:" + strings.Repeat("a", 64)
	current := repository.FederationPlatformSnapshot{
		ProviderID: request.ProviderID,
		EngineInstanceID: request.EngineInstanceID,
		Scope: repository.FederationPlatformScope{
			OrganisationID: request.OrganisationID,
			EstateID: request.EstateID,
		},
		RuntimeCapability: "WORKLOAD_TOKEN_ISSUANCE",
		ProviderStatus: "ACTIVE",
		InstanceStatus: "ACTIVE",
		BindingStatus: "ACTIVE",
		SupportStatus: "VERIFIED",
		ArtifactDigest: sha,
		DeployedArtifactDigest: sha,
		ProfileRevision: 3,
		EvidenceExpiresAt: time.Now().Add(time.Hour),
	}
	if !exactN04RuntimeReadback(request, current) {
		t.Fatal("current registered development workload issuer denied")
	}
	for name, mutate := range map[string]func(*repository.FederationPlatformSnapshot){
		"wrong provider": func(v *repository.FederationPlatformSnapshot) { v.ProviderID = "provider_foreign" },
		"wrong instance": func(v *repository.FederationPlatformSnapshot) { v.EngineInstanceID = "ei_foreign" },
		"wrong organisation": func(v *repository.FederationPlatformSnapshot) { v.Scope.OrganisationID = "org_other" },
		"wrong estate": func(v *repository.FederationPlatformSnapshot) { v.Scope.EstateID = "estate_other" },
		"wrong facet": func(v *repository.FederationPlatformSnapshot) { v.RuntimeCapability = "HUMAN_AUTHENTICATION" },
		"inactive provider": func(v *repository.FederationPlatformSnapshot) { v.ProviderStatus = "SUSPENDED" },
		"inactive instance": func(v *repository.FederationPlatformSnapshot) { v.InstanceStatus = "SUSPENDED" },
		"withdrawn binding": func(v *repository.FederationPlatformSnapshot) { v.BindingStatus = "REVOKED" },
		"unverified support": func(v *repository.FederationPlatformSnapshot) { v.SupportStatus = "PARTIAL" },
		"artifact drift": func(v *repository.FederationPlatformSnapshot) { v.DeployedArtifactDigest = "sha256:" + strings.Repeat("b", 64) },
		"missing artifact": func(v *repository.FederationPlatformSnapshot) { v.ArtifactDigest = "" },
		"zero profile revision": func(v *repository.FederationPlatformSnapshot) { v.ProfileRevision = 0 },
		"missing evidence deadline": func(v *repository.FederationPlatformSnapshot) { v.EvidenceExpiresAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			substituted := current
			mutate(&substituted)
			if exactN04RuntimeReadback(request, substituted) {
				t.Fatal("untrusted N04 CP runtime projection admitted")
			}
		})
	}
}
