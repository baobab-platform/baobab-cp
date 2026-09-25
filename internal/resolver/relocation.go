package resolver

import (
	"errors"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// RelocationPlan is an atomic topology transition: the prior binding is
// closed at CutoverAt and its successor begins at the same instant.
type RelocationPlan struct {
	Previous  capabilitydomain.CapabilityBinding
	Successor capabilitydomain.CapabilityBinding
	CutoverAt time.Time
}

// PlanEngineRelocation creates a temporally contiguous binding transition.
// Canonical entities and external references are intentionally absent: an
// infrastructure move must never rewrite business identity.
func PlanEngineRelocation(binding capabilitydomain.CapabilityBinding, from, to domain.EngineInstance, cutover time.Time) (RelocationPlan, error) {
	if cutover.IsZero() || binding.EngineInstanceID != from.ID || from.EngineID == "" || from.EngineID != to.EngineID {
		return RelocationPlan{}, errors.New("invalid engine relocation")
	}
	if to.Status != "ACTIVE" || (to.HealthStatus != "" && to.HealthStatus != "UNKNOWN" && to.HealthStatus != "HEALTHY") {
		return RelocationPlan{}, errors.New("relocation target is not eligible")
	}
	if !to.EffectiveFrom.IsZero() && cutover.Before(to.EffectiveFrom) {
		return RelocationPlan{}, errors.New("relocation target is not yet effective")
	}
	if to.EffectiveTo != nil && !cutover.Before(*to.EffectiveTo) {
		return RelocationPlan{}, errors.New("relocation target is expired")
	}

	previous := binding
	previous.Status = "INACTIVE"
	previous.EffectiveTo = &cutover
	successor := binding
	successor.ID = ""
	successor.EngineInstanceID = to.ID
	successor.Status = "ACTIVE"
	successor.EffectiveFrom = cutover
	successor.EffectiveTo = nil
	successor.Version++
	return RelocationPlan{Previous: previous, Successor: successor, CutoverAt: cutover}, nil
}
