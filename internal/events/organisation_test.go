package events

import (
	"strings"
	"testing"
)

func TestEventTypePrefixes(t *testing.T) {
	base := Params{Source: "https://control-plane.baobab-platform.com", Subject: "organisation/x",
		DataSchema:    "https://contracts.baobab-platform.com/organisation/v1/events.schema.json#/$defs/OrganisationCreated",
		CorrelationID: "0192a1b0-7c3e-7a10-8000-0000000000ff", Data: map[string]any{"organisation_id": "x"}}
	for typ, ok := range map[string]bool{
		OrganisationCreated: true, // canonical, ADR-SHARED-008
		"com.nabhold.control-plane.tenant-provisioning-started.v1": false, // legacy namespace, rejected
		"baobab.control-plane.organisation.created.v1":             false,
		"com.example.control-plane.organisation.created.v1":        false,
		"com.baobab-platform.control-plane.organisation.created":   false,
	} {
		p := base
		p.Type = typ
		if _, err := New(p); (err == nil) != ok {
			t.Errorf("New(type=%q) error=%v; want accepted=%v", typ, err, ok)
		}
	}
}

func TestEveryOrganisationEventHasAPayloadDefinition(t *testing.T) {
	for _, typ := range []string{OrganisationCreated, OrganisationVerified, OrganisationSuspended, LegalEntityVerified,
		CorporateRelationshipActivated, CorporateRelationshipEnded, CorporateRelationshipConflicted,
		PlatformRelationshipActivated, PlatformRelationshipEnded, PlatformAccountCreated,
		PlatformAccountMembershipChanged, TenantOrganisationMappingActivated, TenantLegalEntityMappingActivated} {
		if !strings.HasPrefix(typ, "com.baobab-platform.control-plane.") || OrganisationPayloadDef(typ) == "" {
			t.Errorf("%s: not canonical or has no payload $def", typ)
		}
	}
	env, err := NewOrganisationEnvelope(OrganisationChange{EventType: TenantOrganisationMappingActivated, Target: "tenant-organisation-mapping/tom_x",
		TenantID: "tn_01k8z3v1food", Data: map[string]any{"tenant_id": "tn_01k8z3v1food"}}, "https://control-plane.baobab-platform.com", "0192a1b0-7c3e-7a10-8000-0000000000ff")
	if err != nil {
		t.Fatal(err)
	}
	if env.BaobabScope != ScopeTenant || !strings.HasSuffix(env.DataSchema, "#/$defs/TenantOrganisationMappingActivated") {
		t.Fatalf("tenant mapping envelope: scope=%s dataschema=%s", env.BaobabScope, env.DataSchema)
	}
}
