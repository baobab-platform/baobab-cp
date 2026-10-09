package organisation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"gopkg.in/yaml.v3"
)

// FirstPartyRegistryPath is the registry's path inside baobab-platform/shared
// and inside the published contracts image (/baobab/contracts/...).
const FirstPartyRegistryPath = "contracts/legal-entity/registry.yaml"

// acceptedRegistrySchemas are the registry schema names this loader
// understands; the second is the name before the GitHub organisation rename.
var acceptedRegistrySchemas = map[string]bool{
	"baobab-platform-legal-entity-registry": true,
	"nabhold-legal-entity-registry":         true,
}

// FirstPartyEntity is one first-party governance record (ADR-BCP-018 section 13).
type FirstPartyEntity struct {
	ID        string
	LegalName string
	Role      string
    IdentityClass string
    IncorporationClaim string
}

// FirstPartyRegistry is a parsed Shared legal-entity registry. Digest is the
// sha256 of the exact file content, so every reconciled fact can cite the
// registry revision it came from.
type FirstPartyRegistry struct {
	Entities []FirstPartyEntity
	Digest   string
}

// EvidenceReference cites one entity in this registry revision.
func (r FirstPartyRegistry) EvidenceReference(id string) string {
	return "shared:" + FirstPartyRegistryPath + "#" + id + "@sha256:" + r.Digest
}

// LoadFirstPartyRegistry reads and validates a Shared legal-entity registry.
// It fails closed on an unknown schema, a malformed or duplicate id, or a
// missing legal name, rather than reconciling a partial registry.
func LoadFirstPartyRegistry(path string) (FirstPartyRegistry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FirstPartyRegistry{}, err
	}
	return ParseFirstPartyRegistry(raw)
}

// ParseFirstPartyRegistry is LoadFirstPartyRegistry for in-memory content.
func ParseFirstPartyRegistry(raw []byte) (FirstPartyRegistry, error) {
	var doc struct {
		Schema struct {
			Name string `yaml:"name"`
		} `yaml:"schema"`
		Entities []struct {
			ID        string `yaml:"id"`
			LegalName string `yaml:"legal_name"`
			Role      string `yaml:"role"`
            IdentityClass string `yaml:"identity_class"`
            IncorporationClaim string `yaml:"incorporation_claim"`
		} `yaml:"entities"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return FirstPartyRegistry{}, fmt.Errorf("parse first-party registry: %w", err)
	}
	if !acceptedRegistrySchemas[doc.Schema.Name] {
		return FirstPartyRegistry{}, fmt.Errorf("unsupported legal-entity registry schema %q", doc.Schema.Name)
	}
	if len(doc.Entities) == 0 {
		return FirstPartyRegistry{}, errors.New("first-party registry has no entities")
	}
	sum := sha256.Sum256(raw)
	reg := FirstPartyRegistry{Digest: hex.EncodeToString(sum[:])}
	seen := map[string]bool{}
	for _, e := range doc.Entities {
		if len(e.ID) < 3 || len(e.ID) > 63 || !domain.IsCanonicalLegalEntityID(e.ID) {
			return FirstPartyRegistry{}, fmt.Errorf("first-party registry: invalid legal entity id %q", e.ID)
		}
		if seen[e.ID] {
			return FirstPartyRegistry{}, fmt.Errorf("first-party registry: duplicate legal entity id %q", e.ID)
		}
		if e.LegalName == "" {
			return FirstPartyRegistry{}, fmt.Errorf("first-party registry: %s has no legal_name", e.ID)
		}
        // Legacy registries without annotations are readable for historical
        // compatibility. Explicitly annotated identities cannot claim
        // a separately incorporated LegalEntity solely from group status.
        if (e.IdentityClass=="") != (e.IncorporationClaim=="") {
            return FirstPartyRegistry{},fmt.Errorf("first-party registry: %s requires identity_class and incorporation_claim together",e.ID)
        }
        if e.IdentityClass!="" {
            if e.IdentityClass!="OPERATING_BUSINESS" && e.IdentityClass!="LEGAL_PERSON" {
                return FirstPartyRegistry{},fmt.Errorf("first-party registry: %s invalid identity_class",e.ID)
            }
            switch e.IncorporationClaim {
            case "NOT_INCORPORATED","REGISTERED_CLAIMED","REGISTERED_EVIDENCED":
            default:
                return FirstPartyRegistry{},fmt.Errorf("first-party registry: %s invalid incorporation_claim",e.ID)
            }
            if e.IdentityClass=="OPERATING_BUSINESS" && e.IncorporationClaim!="NOT_INCORPORATED"{
                return FirstPartyRegistry{},fmt.Errorf("first-party registry: %s cannot claim incorporation as an operating business",e.ID)
            }
        }
		seen[e.ID] = true
		reg.Entities = append(reg.Entities, FirstPartyEntity{
            ID:e.ID,LegalName:e.LegalName,Role:e.Role,
            IdentityClass:e.IdentityClass,IncorporationClaim:e.IncorporationClaim,
        })
	}
	return reg, nil
}
