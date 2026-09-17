package domain

import (
	"testing"
	"time"
)

func TestMarketValidate(t *testing.T) {
	valid := Market{Code: "ZA", Name: "South Africa", Currency: "ZAR", Region: "af-south-1", IsActive: true}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid market rejected: %v", err)
	}

	missingCode := valid
	missingCode.Code = ""
	if err := missingCode.Validate(); err == nil {
		t.Fatal("expected missing code to be rejected")
	}

	missingName := valid
	missingName.Name = ""
	if err := missingName.Validate(); err == nil {
		t.Fatal("expected missing name to be rejected")
	}

	badCurrency := valid
	badCurrency.Currency = "zar"
	if err := badCurrency.Validate(); err == nil {
		t.Fatal("expected a lowercase currency code to be rejected")
	}
	badCurrency.Currency = "ZARX"
	if err := badCurrency.Validate(); err == nil {
		t.Fatal("expected a 4-letter currency code to be rejected")
	}

	missingRegion := valid
	missingRegion.Region = ""
	if err := missingRegion.Validate(); err == nil {
		t.Fatal("expected missing region to be rejected")
	}
}

func TestMarketAssignmentValidate(t *testing.T) {
	now := time.Now().UTC()
	valid := MarketAssignment{TenantID: "tn_zuribeans", MarketID: "market-1", Capabilities: []MarketParticipationCapability{MarketParticipationSelling}, EffectiveFrom: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid market assignment rejected: %v", err)
	}

	missingTenant := valid
	missingTenant.TenantID = ""
	if err := missingTenant.Validate(); err == nil {
		t.Fatal("expected missing tenant_id to be rejected")
	}

	missingMarket := valid
	missingMarket.MarketID = ""
	if err := missingMarket.Validate(); err == nil {
		t.Fatal("expected missing market_id to be rejected")
	}

	missingFrom := valid
	missingFrom.EffectiveFrom = time.Time{}
	if err := missingFrom.Validate(); err == nil {
		t.Fatal("expected missing effective_from to be rejected")
	}

	before := valid
	badTo := now.Add(-time.Hour)
	before.EffectiveTo = &badTo
	if err := before.Validate(); err == nil {
		t.Fatal("expected effective_to at or before effective_from to be rejected")
	}

	after := valid
	goodTo := now.Add(time.Hour)
	after.EffectiveTo = &goodTo
	if err := after.Validate(); err != nil {
		t.Fatalf("valid effective_to rejected: %v", err)
	}

	noCapabilities := valid
	noCapabilities.Capabilities = nil
	if err := noCapabilities.Validate(); err == nil {
		t.Fatal("expected a market assignment with no participation capabilities to be rejected")
	}

	invalidCapability := valid
	invalidCapability.Capabilities = []MarketParticipationCapability{"BOGUS"}
	if err := invalidCapability.Validate(); err == nil {
		t.Fatal("expected an invalid participation capability to be rejected")
	}

	duplicateCapability := valid
	duplicateCapability.Capabilities = []MarketParticipationCapability{MarketParticipationSelling, MarketParticipationSelling}
	if err := duplicateCapability.Validate(); err == nil {
		t.Fatal("expected a duplicate participation capability to be rejected")
	}

	multipleIndependentCapabilities := valid
	multipleIndependentCapabilities.LegalEntityID = "ZURIBEANS-UG"
	// ADR-BCP-011 §71: every capability is explicit -- EXPORTING must never
	// imply IMPORTING, so a market assignment naming only one of a pair
	// must validate without the other being inferred or required.
	multipleIndependentCapabilities.Capabilities = []MarketParticipationCapability{MarketParticipationExporting}
	if err := multipleIndependentCapabilities.Validate(); err != nil {
		t.Fatalf("expected EXPORTING alone (without IMPORTING) to be valid, got: %v", err)
	}
}
