package domain

import (
	"testing"
	"time"
)

func TestProductValidateAccepts(t *testing.T) {
	p := Product{ID: "baobab-xbt", Name: "Baobab Cross-Border Trade", Status: ProductLifecycleActive}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid product, got: %v", err)
	}
}

func TestProductValidateRejectsInvalidID(t *testing.T) {
	p := Product{ID: "Not Valid!", Name: "x", Status: ProductLifecycleActive}
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of an invalid product_id")
	}
}

func TestProductValidateRequiresName(t *testing.T) {
	p := Product{ID: "baobab-xbt", Status: ProductLifecycleActive}
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of a missing name")
	}
}

func validProductVersion() ProductVersion {
	return ProductVersion{ProductID: "baobab-xbt", Version: "1.0.0", CompositionKey: "solution.baobab-xbt", Status: ProductLifecycleActive}
}

func TestProductVersionValidateAccepts(t *testing.T) {
	if err := validProductVersion().Validate(); err != nil {
		t.Fatalf("expected valid product version, got: %v", err)
	}
}

func TestProductVersionValidateRejectsBadSemver(t *testing.T) {
	v := validProductVersion()
	v.Version = "1.0"
	if err := v.Validate(); err == nil {
		t.Fatal("expected rejection of a non-semver version")
	}
}

func TestProductVersionValidateRequiresDeprecatedAt(t *testing.T) {
	v := validProductVersion()
	v.Status = ProductLifecycleDeprecated
	if err := v.Validate(); err == nil {
		t.Fatal("expected rejection of DEPRECATED status without deprecated_at")
	}
	now := time.Now().UTC()
	v.DeprecatedAt = &now
	if err := v.Validate(); err != nil {
		t.Fatalf("expected DEPRECATED with deprecated_at to be valid, got: %v", err)
	}
}
