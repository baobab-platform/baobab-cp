package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/contracttest"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
	"gopkg.in/yaml.v3"
)

func readDefs(t *testing.T, path string) map[string]struct {
	Enum       []string       `json:"enum"`
	Properties map[string]any `json:"properties"`
} {
	t.Helper()
	var doc struct {
		Defs map[string]struct {
			Enum       []string       `json:"enum"`
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Defs
}

// TestSubscriptionClassificationIsSharedVocabulary: the Control Plane's
// subscription types, classification sources, drift rules and the
// subscription.classified event are exactly what Shared publishes
// (ADR-SHARED-011).
func TestSubscriptionClassificationIsSharedVocabulary(t *testing.T) {
	contracts := filepath.Join(contracttest.SharedDir(t), "contracts")
	product := readDefs(t, filepath.Join(contracts, "product", "v1", "domain.schema.json"))
	for _, s := range product["subscriptionType"].Enum {
		if !domain.SubscriptionType(s).Valid() {
			t.Errorf("subscription type %s is not known to the Control Plane", s)
		}
	}
	if len(product["subscriptionType"].Enum) != 6 {
		t.Fatalf("subscription types: %v", product["subscriptionType"].Enum)
	}
	sources := []string{string(domain.ClassificationFromAdmissionDecision), string(domain.ClassificationFromReclassification),
		string(domain.ClassificationFromMigration), string(domain.ClassificationFromManualGovernance)}
	if !slices.Equal(product["classificationSource"].Enum, sources) {
		t.Fatalf("classification sources %v, want %v", product["classificationSource"].Enum, sources)
	}

	observability := readDefs(t, filepath.Join(contracts, "organisation", "v1", "observability.schema.json"))
	for _, rule := range []string{domain.DriftAffiliateBasisNotInForce, domain.DriftGroupMembershipBasisNotInForce,
		domain.DriftRelationshipPastEffectiveTo, domain.DriftTenantMappingToInactiveOrg, domain.DriftIamReferenceToInactiveOrg,
		domain.DriftAccountMembershipOnInactive, domain.DriftCounterpartyRoleOnInactiveOrg, domain.DriftInternalClassificationBasisNotInForce} {
		if !slices.Contains(observability["driftRule"].Enum, rule) {
			t.Errorf("drift rule %s is not in Shared", rule)
		}
	}
	if !slices.Contains(observability["driftResourceType"].Enum, "PRODUCT_SUBSCRIPTION") {
		t.Error("PRODUCT_SUBSCRIPTION is not a Shared drift resource type")
	}

	var asyncapi struct {
		Components struct {
			Messages map[string]struct {
				Name    string `yaml:"name"`
				Payload struct {
					AllOf []struct {
						Properties map[string]struct {
							Ref string `yaml:"$ref"`
						} `yaml:"properties"`
					} `yaml:"allOf"`
				} `yaml:"payload"`
			} `yaml:"messages"`
		} `yaml:"components"`
	}
	raw, err := os.ReadFile(filepath.Join(contracts, "product", "v1", "asyncapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &asyncapi); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range asyncapi.Components.Messages {
		if message.Name != events.ProductSubscriptionClassified {
			continue
		}
		for _, part := range message.Payload.AllOf {
			if ref := part.Properties["data"].Ref; ref != "" {
				found = strings.HasSuffix(ref, "#/$defs/"+events.ProductPayloadDef(events.ProductSubscriptionClassified))
			}
		}
	}
	if !found {
		t.Fatalf("%s is not registered in Shared with its payload", events.ProductSubscriptionClassified)
	}
}

// TestApplicationChannelsRemainServerControlled is the ORG-11 regression
// for ADR-BCP-017 section 9: ASSISTED_ENTERPRISE and INTERNAL_GROUP remain
// valid channels, set by the Control Plane for staff-opened applications
// (a route that is not built yet), and never chosen by an applicant: the
// applicant's draft has no channel field.
func TestApplicationChannelsRemainServerControlled(t *testing.T) {
	admission := readDefs(t, filepath.Join(contracttest.SharedDir(t), "contracts", "admission", "v1", "application.schema.json"))
	want := []string{string(domain.ChannelSelfService), string(domain.ChannelAssistedEnterprise), string(domain.ChannelInternalGroup)}
	if !slices.Equal(admission["applicationChannel"].Enum, want) {
		t.Fatalf("application channels %v, want %v", admission["applicationChannel"].Enum, want)
	}
	if _, ok := admission["ClientApplicationDraft"].Properties["application_channel"]; ok {
		t.Fatal("an applicant's draft must not carry application_channel")
	}
	if _, ok := admission["ClientApplication"].Properties["application_channel"]; !ok {
		t.Fatal("a ClientApplication records its channel")
	}
}
