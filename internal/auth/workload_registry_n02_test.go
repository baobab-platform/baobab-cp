package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReconciledCandidateMatchesReviewedSharedBytes(t *testing.T) {
	root := filepath.Join("..", "contracts", "candidates", "n02")
	raw, err := os.ReadFile(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		SHA                string `json:"source_sha"`
		Digest             string `json:"sha256"`
		SourceReviewOnly   bool   `json:"source_review_only"`
		RuntimeAuthority   bool   `json:"runtime_authority"`
		AcceptedSharedMain bool   `json:"accepted_shared_main"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if source.SHA != "7f547ccdd759208355ef9224d2177929f4bf224e" ||
		!source.SourceReviewOnly || source.RuntimeAuthority || !source.AcceptedSharedMain {
		t.Fatal("unreviewed or authorizing candidate provenance")
	}
	registry, err := os.ReadFile(filepath.Join(root, "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(registry)
	if hex.EncodeToString(digest[:]) != source.Digest {
		t.Fatal("candidate bytes differ from accepted Shared #430")
	}
	// A candidate remains read-only review material. Accepted runtime bytes must
	// be independently copied from the merged Shared main commit, never from
	// a private PR branch or a runtime-client registration operation.
	accepted, err := os.ReadFile(filepath.Join("..", "contracts", "shared", "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(registry, accepted) {
		t.Fatal("accepted CP workload bytes drift from independently reviewed candidate")
	}
	lock, err := os.ReadFile(filepath.Join("..", "..", "contracts.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Candidate N02 is immutable review evidence at source.SHA, while the
	// active CP contract pin must advance with independently accepted Shared
	// main. Requiring both to be the old N02 SHA blocks every legitimate
	// subsequent Shared upgrade (e.g. F1/People/WMS), not a security change.
	// Their exact bytes are already compared above; the lock's authority and
	// ancestry are independently checked by Foundation.
	commitLines := regexp.MustCompile(`(?m)^(?:source_commit:|  commit:) ([0-9a-f]{40})package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReconciledCandidateMatchesReviewedSharedBytes(t *testing.T) {
	root := filepath.Join("..", "contracts", "candidates", "n02")
	raw, err := os.ReadFile(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		SHA                string `json:"source_sha"`
		Digest             string `json:"sha256"`
		SourceReviewOnly   bool   `json:"source_review_only"`
		RuntimeAuthority   bool   `json:"runtime_authority"`
		AcceptedSharedMain bool   `json:"accepted_shared_main"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if source.SHA != "7f547ccdd759208355ef9224d2177929f4bf224e" ||
		!source.SourceReviewOnly || source.RuntimeAuthority || !source.AcceptedSharedMain {
		t.Fatal("unreviewed or authorizing candidate provenance")
	}
	registry, err := os.ReadFile(filepath.Join(root, "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(registry)
	if hex.EncodeToString(digest[:]) != source.Digest {
		t.Fatal("candidate bytes differ from accepted Shared #430")
	}
	// A candidate remains read-only review material. Accepted runtime bytes must
	// be independently copied from the merged Shared main commit, never from
	// a private PR branch or a runtime-client registration operation.
	accepted, err := os.ReadFile(filepath.Join("..", "contracts", "shared", "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(registry, accepted) {
		t.Fatal("accepted CP workload bytes drift from independently reviewed candidate")
	}
	lock, err := os.ReadFile(filepath.Join("..", "..", "contracts.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
).FindAllStringSubmatch(string(lock), -1)
	if len(commitLines) != 2 || commitLines[0][1] != commitLines[1][1] {
		t.Fatal("both CP contract lock fields must agree on one immutable Shared main commit")
	}
}

// IAM-N02 is a DEVELOPMENT-ONLY candidate projection of the reviewed Shared
// workload entry. Registration is not activation, issuer credentials, CP
// Principal creation or purchasing authority. Shared #430 is an accepted
// contract source, but its new development identities are not runtime-authorized.
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
	// N02/N04/N07 declarations are now included in accepted Shared main, but
	// their PROVISIONED status must never grant runtime workload admission.
	// A signed workload token is not itself a CP Principal or business grant.
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
			len(runtime.SubjectPrincipalAudiences(name)) != 0 ||
			runtime.AllowsEnvironment(name, "development") ||
			runtime.AllowsEnvironment(name, "staging") ||
			runtime.AllowsEnvironment(name, "production") {
			t.Fatalf("PROVISIONED Shared identity %s gained CP runtime admission", name)
		}
		for _, scope := range []string{"identity-runtime:read", "context:resolve",
			ContextValidateScope, "legal-actor:assess", "tms:booking:submit"} {
			if runtime.AllowsScope(name, scope) {
				t.Fatalf("PROVISIONED Shared identity %s gained %s", name, scope)
			}
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
