package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReconciledCandidateMatchesReviewedSharedBytes(t *testing.T) {
	root := filepath.Join("..", "contracts", "candidates", "n02")
	raw, err := os.ReadFile(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		SHA              string `json:"source_sha"`
		Digest           string `json:"sha256"`
		SourceReviewOnly bool   `json:"source_review_only"`
		RuntimeAuthority bool   `json:"runtime_authority"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if source.SHA != "1dc7a7eba0c197159473864c5cd8bea7637e4a8c" ||
		!source.SourceReviewOnly || source.RuntimeAuthority {
		t.Fatal("unreviewed or authorizing candidate provenance")
	}
	registry, err := os.ReadFile(filepath.Join(root, "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(registry)
	if hex.EncodeToString(digest[:]) != source.Digest {
		t.Fatal("candidate bytes differ from reviewed Shared #430")
	}
}

// IAM-N02 is a DEVELOPMENT-ONLY candidate projection of the reviewed Shared
// workload entry. Registration is not activation, issuer credentials, CP
// Principal creation or purchasing authority. The default CP source pin
// remains independently review-gated while consolidated Shared #430 is a draft.
func TestN02ProposedEstatePrincipalReaderCannotCreateAuthority(t *testing.T) {
	path := filepath.Join("..", "contracts", "candidates", "n02", "identity", "v1", "workload-registry.yaml")
	registry, err := LoadWorkloadRegistryFile(path)
	if err != nil {
		t.Fatal("cannot parse candidate Shared workload source", err)
	}
	const candidate = "baobab-dev-zuribeans-cp-principal-validator"
	if registry.IsActive(candidate) {
		t.Fatal("PROVISIONED estate CP reader became ACTIVE")
	}
	if registry.AllowsScope(candidate, PrincipalResolveScope) ||
		registry.AllowsScope(candidate, PrincipalRegisterBuyerScope) ||
		registry.AllowsScope(candidate, ContextValidateScope) {
		t.Fatal("unprovisioned estate caller gained CP workload scope")
	}
	if registry.AllowsEnvironment(candidate, "development") ||
		registry.AllowsEnvironment(candidate, "staging") ||
		registry.AllowsEnvironment(candidate, "production") {
		t.Fatal("PROVISIONED caller passed workload/environment admission")
	}
	if audience := registry.SubjectPrincipalAudiences(candidate); len(audience) != 0 {
		t.Fatalf("PROVISIONED caller gained human subject delegation: %v", audience)
	}
	// The independently owned Next commerce BFF is Trade-audience only and
	// cannot inherit the new CP reader's identity:principal:resolve scope.
	const existing = "baobab-dev-zuribeans-bff"
	if !registry.IsActive(existing) ||
		registry.AllowsScope(existing, PrincipalResolveScope) ||
		registry.AllowsScope(existing, PrincipalRegisterBuyerScope) ||
		len(registry.SubjectPrincipalAudiences(existing)) != 0 {
		t.Fatal("existing Next commerce BFF inherited new native CP privilege")
	}
	// N02's actual authorization-code browser app is not a WORKLOAD entry,
	// and cannot use this registry as a substitute for real Hydra registration.
	const browser = "baobab-dev-zuribeans-native-browser-bff"
	if registry.IsActive(browser) || registry.AllowsScope(browser, PrincipalResolveScope) {
		t.Fatal("human OAuth browser client was treated as an ACTIVE CP workload")
	}
}

func TestN02PinnedCandidateCannotActivatePrincipalReader(t *testing.T) {
	// N02 is an inert candidate: Shared #421 is not merged into
	// Shared main, so CP's accepted contract lock/embedded registry must
	// stay unchanged. A signed workload token is not a CP grant or Principal.
	// Candidate data may never activate runtime source authority.
	accepted := filepath.Join("..", "contracts", "shared", "identity", "v1", "workload-registry.yaml")
	runtime, err := LoadWorkloadRegistryFile(accepted)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"baobab-dev-iam-runtime-reader",
		"baobab-dev-zuribeans-cp-principal-validator",
		"baobab-dev-zuribeans-native-browser-bff",
		"baobab-dev-tms-subject",
		"baobab-dev-tms-validator",
	} {
		if runtime.IsActive(name) || runtime.AllowsScope(name, PrincipalResolveScope) ||
			len(runtime.SubjectPrincipalAudiences(name)) != 0 {
			t.Fatalf("draft N02 client %s leaked into accepted CP runtime", name)
		}
	}
}

// Shared #430 reconciles N02/N04/N07 declarations into one inert candidate.
// None of its proposed clients may become a runtime reader, subject validator,
// context validator or transport committer just because the parser accepts it.
func TestReconciledDevelopmentCandidateCannotAdmitAnyProposedClient(t *testing.T) {
	path := filepath.Join("..", "contracts", "candidates", "n02", "identity", "v1", "workload-registry.yaml")
	registry, err := LoadWorkloadRegistryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"baobab-dev-iam-runtime-reader",
		"baobab-dev-zuribeans-cp-principal-validator",
		"baobab-dev-tms-subject",
		"baobab-dev-tms-validator",
		"baobab-dev-zuribeans-native-browser-bff",
	} {
		t.Run(name, func(t *testing.T) {
			if registry.IsActive(name) || len(registry.ValidatesAudiences(name)) != 0 ||
				len(registry.SubjectPrincipalAudiences(name)) != 0 {
				t.Fatal("inert candidate gained runtime or delegation authority")
			}
			for _, scope := range []string{
				"identity-runtime:read", PrincipalResolveScope, PrincipalRegisterBuyerScope,
				"context:resolve", ContextValidateScope, "legal-actor:assess", "tms:booking:submit",
			} {
				if registry.AllowsScope(name, scope) {
					t.Fatalf("PROVISIONED client gained %s", scope)
				}
			}
			for _, environment := range []string{"", "development", "staging", "production"} {
				if registry.AllowsEnvironment(name, environment) {
					t.Fatalf("PROVISIONED client admitted in %q", environment)
				}
			}
		})
	}
}
