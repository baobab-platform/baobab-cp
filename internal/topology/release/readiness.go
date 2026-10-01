package release

import "sort"

// Readiness effects of release drift (release-policy.yaml
// drift.reasons.*.readiness_effect, ADR-BCP-025 section 2.8).
const (
	EffectBlocked  = "BLOCKED"
	EffectDegraded = "DEGRADED"
)

// BoundInstance is one usable binding of a capability for a tenant, and the
// open release drift of the instance behind it ("" if none).
type BoundInstance struct {
	EngineInstanceID string
	// DriftReason is the instance's open drift reason, "" when it has none.
	DriftReason string
	// DriftEffect is that reason's readiness_effect.
	DriftEffect string
}

// ReadinessFinding is one drift reason that touches a capability.
type ReadinessFinding struct {
	Reason            string
	EngineInstanceIDs []string
}

// CapabilityReadiness is the readiness consequence of release drift for one
// capability of one tenant.
type CapabilityReadiness struct {
	CapabilityKey string
	Mandatory     bool
	// Effect is EffectBlocked, EffectDegraded or "" (unaffected).
	Effect   string
	Findings []ReadinessFinding
}

// ReleaseReadinessOf judges one capability from its usable bindings.
//
// Release drift blocks only a mandatory capability, and only when every
// usable binding is on an instance whose drift has a BLOCKED effect: with an
// unaffected (or merely degraded) binding left, the capability can still be
// served and is DEGRADED. An optional capability is never blocked by drift.
// Any drift touching the capability, whatever its class, is at least
// DEGRADED and visible. A capability with no usable binding says nothing
// about drift: that is a binding finding, not a release one.
func ReleaseReadinessOf(capabilityKey string, mandatory bool, bindings []BoundInstance) CapabilityReadiness {
	out := CapabilityReadiness{CapabilityKey: capabilityKey, Mandatory: mandatory}
	if len(bindings) == 0 {
		return out
	}
	byReason := map[string][]string{}
	allBlocked := true
	for _, b := range bindings {
		if b.DriftReason == "" {
			allBlocked = false
			continue
		}
		if b.DriftEffect != EffectBlocked {
			allBlocked = false
		}
		byReason[b.DriftReason] = append(byReason[b.DriftReason], b.EngineInstanceID)
	}
	if len(byReason) == 0 {
		return out
	}
	reasons := make([]string, 0, len(byReason))
	for reason := range byReason {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		ids := byReason[reason]
		sort.Strings(ids)
		out.Findings = append(out.Findings, ReadinessFinding{Reason: reason, EngineInstanceIDs: ids})
	}
	out.Effect = EffectDegraded
	if mandatory && allBlocked {
		out.Effect = EffectBlocked
	}
	return out
}

// TenantReleaseReadiness is the tenant's release-drift readiness: the
// capabilities it affects and the verdict they imply.
type TenantReleaseReadiness struct {
	// Effect is EffectBlocked when any mandatory capability is blocked,
	// EffectDegraded when anything is affected, "" otherwise.
	Effect       string
	Capabilities []CapabilityReadiness
}

// Aggregate combines capability verdicts, keeping only the affected ones in
// capability-key order.
func Aggregate(capabilities []CapabilityReadiness) TenantReleaseReadiness {
	var out TenantReleaseReadiness
	for _, c := range capabilities {
		if c.Effect == "" {
			continue
		}
		out.Capabilities = append(out.Capabilities, c)
		switch {
		case c.Effect == EffectBlocked:
			out.Effect = EffectBlocked
		case out.Effect == "":
			out.Effect = EffectDegraded
		}
	}
	sort.Slice(out.Capabilities, func(i, j int) bool { return out.Capabilities[i].CapabilityKey < out.Capabilities[j].CapabilityKey })
	return out
}
