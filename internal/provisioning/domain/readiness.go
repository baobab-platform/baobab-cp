package domain

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// ReadinessCheck is durable evidence that one prerequisite for activation
// has been evaluated. Checks are deliberately generic: the control plane
// orchestrates authoritative engines; it does not absorb their domains.
type ReadinessCheck struct {
	Name      string    `json:"name"`
	Ready     bool      `json:"ready"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// ProvisioningReadiness is the fail-closed activation decision for one
// TenantProvisioning attempt.
type ProvisioningReadiness struct {
	ProvisioningID string           `json:"provisioning_id"`
	Checks         []ReadinessCheck `json:"checks"`
}

var requiredReadinessChecks = []string{
	"tenant",
	"legal_entity",
	"market_participation",
	"digital_estate",
	"capability_grants",
	"capability_bindings",
	"engine_instances",
	"trade_lanes",
	"isolation_profile",
	"context_resolution",
	"desired_observed_state",
}

func RequiredReadinessChecks() []string {
	out := append([]string(nil), requiredReadinessChecks...)
	sort.Strings(out)
	return out
}

func (r ProvisioningReadiness) Validate() error {
	if strings.TrimSpace(r.ProvisioningID) == "" {
		return errors.New("provisioning_id is required")
	}
	seen := make(map[string]struct{}, len(r.Checks))
	for _, check := range r.Checks {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			return errors.New("readiness check name is required")
		}
		if check.CheckedAt.IsZero() {
			return errors.New("readiness checked_at is required")
		}
		if _, exists := seen[name]; exists {
			return errors.New("duplicate readiness check: " + name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// Ready returns true only when every ZB-02 prerequisite is present and
// successful. Unknown or missing checks therefore fail closed.
func (r ProvisioningReadiness) Ready() bool {
	if r.Validate() != nil {
		return false
	}
	byName := make(map[string]ReadinessCheck, len(r.Checks))
	for _, check := range r.Checks {
		byName[check.Name] = check
	}
	for _, required := range requiredReadinessChecks {
		check, ok := byName[required]
		if !ok || !check.Ready {
			return false
		}
	}
	return true
}

func (r ProvisioningReadiness) BlockingReasons() []string {
	byName := make(map[string]ReadinessCheck, len(r.Checks))
	for _, check := range r.Checks {
		byName[check.Name] = check
	}
	var reasons []string
	for _, required := range requiredReadinessChecks {
		check, ok := byName[required]
		if !ok {
			reasons = append(reasons, required+": missing readiness evidence")
			continue
		}
		if !check.Ready {
			reason := strings.TrimSpace(check.Reason)
			if reason == "" {
				reason = "not ready"
			}
			reasons = append(reasons, required+": "+reason)
		}
	}
	return reasons
}
