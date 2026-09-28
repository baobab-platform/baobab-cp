package changeset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Target is the authoritative state of the tenant a changeset names, read
// when it is planned.
type Target struct {
	Found    bool
	Status   string
	Revision int64
	// LockedBy names the other changesets that have not ended and have
	// been submitted against the same tenant (section 66).
	LockedBy []string
}

// PlanInput identifies the plan to generate.
type PlanInput struct {
	Changeset   Changeset
	Target      Target
	PlanID      string
	PlanVersion int
	Now         time.Time
	TTL         time.Duration
}

// Validation is what validating a changeset against authoritative state
// found: a changeset whose target does not exist can never apply and is
// INVALID; one that conflicts with the target's current state or with
// another open changeset is BLOCKED, which a later submit may clear.
type Validation struct {
	Invalid  []Finding
	Blockers []Finding
}

// Validate checks the desired change against the target.
func Validate(c Changeset, t Target) Validation {
	var v Validation
	kind, ok := Kinds()[c.DesiredChange.Kind]
	switch {
	case !ok:
		v.Invalid = append(v.Invalid, Finding{Code: BlockTargetStateConflict, Message: fmt.Sprintf("Change kind %s is not supported.", c.DesiredChange.Kind)})
	case !t.Found:
		v.Invalid = append(v.Invalid, Finding{Code: BlockTargetNotFound, Message: fmt.Sprintf("Tenant %s does not exist.", c.DesiredChange.TenantID)})
	default:
		if !slices.Contains(kind.FromStatus, t.Status) {
			v.Blockers = append(v.Blockers, Finding{Code: BlockTargetStateConflict,
				Message: fmt.Sprintf("Tenant %s is %s; %s starts from %s.", c.DesiredChange.TenantID, orUnknown(t.Status),
					strings.ToLower(kind.ChangesetType), strings.Join(kind.FromStatus, " or "))})
		}
		for _, other := range t.LockedBy {
			if other != c.ChangesetID {
				v.Blockers = append(v.Blockers, Finding{Code: BlockTargetLocked,
					Message: fmt.Sprintf("Changeset %s already changes tenant %s.", other, c.DesiredChange.TenantID)})
			}
		}
	}
	return v
}

// Generate generates the changeset's plan. It is deterministic: the same
// changeset against the same target yields the same Material. It writes
// nothing (section 20).
func Generate(in PlanInput) (Plan, error) {
	c := in.Changeset
	kind, ok := Kinds()[c.DesiredChange.Kind]
	if !ok {
		return Plan{}, fmt.Errorf("%w: change kind %q is not supported", ErrInvalid, c.DesiredChange.Kind)
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := in.Now.UTC()
	p := Plan{PlanID: in.PlanID, PlanVersion: in.PlanVersion, BaseRevision: max(in.Target.Revision, 1), GeneratedAt: now,
		ExpiresAt: now.Add(ttl), ChangesetID: c.ChangesetID, ChangesetType: kind.ChangesetType, DesiredChange: c.DesiredChange,
		Steps: []Step{}, SecurityChecks: []Check{}, ReadinessRequirements: []Check{}, Blockers: []Finding{}, Warnings: []Finding{}}
	v := Validate(c, in.Target)
	p.Blockers = append(append(p.Blockers, v.Invalid...), v.Blockers...)

	previous := ""
	for _, op := range kind.Operations {
		res := StepResources{TenantID: c.DesiredChange.TenantID, ToStatus: kind.ToStatus}
		if op != OpVerifyTenantState {
			res.FromStatus = in.Target.Status
		}
		deps := []string{}
		if previous != "" {
			deps = []string{previous}
		}
		id := strings.ReplaceAll(strings.ToLower(op), "_", "-")
		p.Steps = append(p.Steps, Step{StepID: id, Operation: op, DependsOn: deps, Resources: res})
		previous = id
	}
	switch kind.ChangesetType {
	case "SUSPEND":
		p.RiskClass = "HIGH"
		p.ImpactAnalysis = ImpactAnalysis{Summary: fmt.Sprintf("Suspends tenant %s: capability resolution for the tenant is refused until it is reinstated.", c.DesiredChange.TenantID),
			ResourcesChanged: 1, AvailabilityImpact: "Every digital estate of the tenant stops resolving capabilities."}
		p.CompensationStrategy = "Reinstate the tenant with a REINSTATE changeset; nothing irreversible is planned."
	default:
		p.RiskClass = "MEDIUM"
		p.ImpactAnalysis = ImpactAnalysis{Summary: fmt.Sprintf("Reinstates tenant %s: capability resolution for the tenant resumes.", c.DesiredChange.TenantID),
			ResourcesChanged: 1, SecurityImpact: "The tenant's administrators and workloads regain access to its capabilities."}
		p.CompensationStrategy = "Suspend the tenant again with a SUSPEND changeset; nothing irreversible is planned."
	}
	p.VerificationStrategy = fmt.Sprintf("Read the tenant back and require desired and observed status %s.", kind.ToStatus)
	p.PlanDigest = PlanDigest(p)
	return p, nil
}

// Material is the digest of a plan's execution semantics: what it would do
// and what it depends on, never its identity, timing or wording
// (section 23). Planning again against changed authoritative state yields
// other Material, which makes the earlier plan stale (section 29).
func Material(p Plan) string {
	codes := make([]string, 0, len(p.Blockers))
	for _, b := range p.Blockers {
		codes = append(codes, b.Code)
	}
	return digestOf(struct {
		ChangesetID   string
		ChangesetType string
		DesiredChange DesiredChange
		BaseRevision  int64
		RiskClass     string
		Steps         []Step
		Blockers      []string
	}{p.ChangesetID, p.ChangesetType, p.DesiredChange, p.BaseRevision, p.RiskClass, p.Steps, codes})
}

// PlanDigest binds one plan version; an approval names it (section 24).
func PlanDigest(p Plan) string {
	return digestOf(struct {
		PlanID      string
		PlanVersion int
		Material    string
	}{p.PlanID, p.PlanVersion, Material(p)})
}

// Stale reports whether a plan no longer matches authoritative state or
// has expired: planning again would produce other material, or its
// validity window has closed (sections 28-30).
func Stale(approved Plan, fresh Plan, now time.Time) bool {
	return !now.Before(approved.ExpiresAt) || Material(approved) != Material(fresh)
}

func digestOf(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic("changeset: digest input is not encodable: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func orUnknown(s string) string {
	if s == "" {
		return "in an unknown state"
	}
	return s
}
