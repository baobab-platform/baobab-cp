package convergence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// digestOf is "sha256:" and the hex SHA-256 of v's JSON encoding. The inputs
// are Go structs, whose encoding has a fixed field order, so equal values
// always have equal digests.
func digestOf(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		// The inputs are plain structs of strings, numbers and slices.
		panic("convergence: digest input is not encodable: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DesiredStateDigest binds the business intent, excluding the digest itself
// and when it was frozen.
func DesiredStateDigest(d DesiredState) string {
	type intent struct {
		Tenant               DesiredTenant
		LegalEntities        []string
		Products             []DesiredProduct
		MarketParticipation  []DesiredMarket
		DigitalEstates       []DesiredEstate
		IsolationRequirement string
		ResidencyRequirement string
		Provenance           DesiredStateProvenance
	}
	return digestOf(intent{d.Tenant, d.LegalEntities, d.Products, d.MarketParticipation, d.DigitalEstates,
		d.IsolationRequirement, d.ResidencyRequirement, d.Provenance})
}

// Material is the digest of a plan's material execution semantics
// (ADR-BCP-021 section 23): what it would do and on what it depends, not
// its identity, timing, warnings or wording. Planning the same desired
// state against unchanged authoritative state yields the same material; a
// different material means the plan is stale.
func Material(p Plan) string {
	type material struct {
		TenantProvisioningID  string
		TenantID              string
		DesiredStateVersion   int64
		DesiredStateDigest    string
		RiskClass             string
		Steps                 []Step
		SecurityChecks        []Check
		ReadinessRequirements []Check
		Blockers              []Finding
	}
	return digestOf(material{p.TenantProvisioningID, p.TenantID, p.DesiredStateVersion, p.DesiredStateDigest,
		p.RiskClass, p.Steps, p.SecurityChecks, p.ReadinessRequirements, p.Blockers})
}

// PlanDigest binds one plan version: its identity, the revision it was
// generated against and its material. An approval names this digest, so a
// materially different plan needs another decision (ADR-BCP-021 section 24).
func PlanDigest(p Plan) string {
	type bound struct {
		PlanID       string
		PlanVersion  int
		BaseRevision int64
		Material     string
	}
	return digestOf(bound{p.PlanID, p.PlanVersion, p.BaseRevision, Material(p)})
}
