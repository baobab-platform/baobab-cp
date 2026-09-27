// Package health decides whether an engine instance, a provider and a
// provider's capability are healthy enough to serve a capability
// (ADR-BCP-006 sections 18-22 and 72-73, ADR-SHARED-007 section 37.1).
//
// Health is a time-bounded observation, never a stored fact: an
// observation counts only while observed_at <= now < expires_at, and a
// subject with no current observation is UNKNOWN. Which effective statuses
// are eligible depends on the capability's health criticality, as
// capability/v1 health-policy.yaml (embedded at the pinned Shared commit)
// says. Capability resolution, engine relocation and provisioning planning
// all decide through Policy.Evaluate, so they cannot disagree.
package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Status is capability/v1 providerHealthStatus.
type Status string

const (
	StatusUnknown     Status = "UNKNOWN"
	StatusHealthy     Status = "HEALTHY"
	StatusDegraded    Status = "DEGRADED"
	StatusUnavailable Status = "UNAVAILABLE"
)

func (s Status) Valid() bool {
	switch s {
	case StatusUnknown, StatusHealthy, StatusDegraded, StatusUnavailable:
		return true
	}
	return false
}

// Criticality is capability/v1 capabilityHealthCriticality: which health a
// capability tolerates. Empty means STANDARD.
type Criticality string

const (
	CriticalityCritical Criticality = "CRITICAL"
	CriticalityStandard Criticality = "STANDARD"
)

// Valid accepts the empty value, which is STANDARD.
func (c Criticality) Valid() bool {
	switch c {
	case "", CriticalityCritical, CriticalityStandard:
		return true
	}
	return false
}

// OrDefault returns STANDARD for the empty value.
func (c Criticality) OrDefault() Criticality {
	if c == "" {
		return CriticalityStandard
	}
	return c
}

// Source is capability/v1 healthObservationSource.
type Source string

const (
	SourceActiveProbe      Source = "ACTIVE_PROBE"
	SourcePassiveTelemetry Source = "PASSIVE_TELEMETRY"
	SourceEngineReport     Source = "ENGINE_REPORT"
	SourceOperator         Source = "OPERATOR"
)

func (s Source) Valid() bool {
	switch s {
	case SourceActiveProbe, SourcePassiveTelemetry, SourceEngineReport, SourceOperator:
		return true
	}
	return false
}

// Level is one of the three levels ADR-BCP-006 section 73 keeps apart.
type Level string

const (
	LevelEngineInstance     Level = "ENGINE_INSTANCE"
	LevelProvider           Level = "PROVIDER"
	LevelProviderCapability Level = "PROVIDER_CAPABILITY"
)

// Subject is what an observation is about: exactly one level.
type Subject struct {
	EngineInstanceID string `json:"engine_instance_id,omitempty"`
	ProviderID       string `json:"provider_id,omitempty"`
	CapabilityKey    string `json:"capability_key,omitempty"`
}

// Level reports the subject's level, or an error when its identifiers do
// not name exactly one.
func (s Subject) Level() (Level, error) {
	switch {
	case s.EngineInstanceID != "" && s.ProviderID == "" && s.CapabilityKey == "":
		return LevelEngineInstance, nil
	case s.EngineInstanceID == "" && s.ProviderID != "" && s.CapabilityKey == "":
		return LevelProvider, nil
	case s.EngineInstanceID == "" && s.ProviderID != "" && s.CapabilityKey != "":
		return LevelProviderCapability, nil
	}
	return "", errors.New("a health subject names exactly one of: an engine instance, a provider, or a provider and one capability")
}

// Observation is capability/v1 HealthObservation.
type Observation struct {
	Subject    Subject   `json:"subject"`
	Status     Status    `json:"status"`
	ObservedAt time.Time `json:"observed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Source     Source    `json:"source"`
	Reasons    []string  `json:"reasons"`
}

// Validate applies what the contract requires of an observation.
func (o Observation) Validate() error {
	if _, err := o.Subject.Level(); err != nil {
		return err
	}
	if !o.Status.Valid() {
		return fmt.Errorf("status %q is not a provider health status", o.Status)
	}
	if !o.Source.Valid() {
		return fmt.Errorf("source %q is not a health observation source", o.Source)
	}
	if o.ObservedAt.IsZero() || !o.ExpiresAt.After(o.ObservedAt) {
		return errors.New("expires_at must be later than observed_at")
	}
	if len(o.Reasons) == 0 && o.Status != StatusHealthy {
		return fmt.Errorf("a %s observation needs at least one reason", o.Status)
	}
	if len(o.Reasons) > 16 {
		return errors.New("an observation carries at most 16 reasons")
	}
	for i, r := range o.Reasons {
		if !reasonCode.MatchString(r) {
			return fmt.Errorf("reason %q is not a reason code", r)
		}
		if slices.Contains(o.Reasons[:i], r) {
			return fmt.Errorf("reason %q is repeated", r)
		}
	}
	return nil
}

// Current reports whether the observation says anything at now.
func (o *Observation) Current(now time.Time) bool {
	return o != nil && !now.Before(o.ObservedAt) && now.Before(o.ExpiresAt)
}

// Effective is the subject's health at now: the observation's status while
// it is current, otherwise UNKNOWN. A missing, expired or future-dated
// observation is UNKNOWN.
func Effective(o *Observation, now time.Time) Status {
	if !o.Current(now) {
		return StatusUnknown
	}
	return o.Status
}

// Levels holds the newest observation the Control Plane has for each level
// of one candidate. A nil Provider or ProviderCapability means none is held,
// and that level is not checked; the engine instance level always is.
type Levels struct {
	EngineInstance     *Observation
	Provider           *Observation
	ProviderCapability *Observation
}

// Decision is the policy's verdict on one candidate.
type Decision struct {
	Eligible bool
	// When not eligible: the first failing level, its effective status and
	// the registered capability_resolution_denial code for it.
	Level      Level
	Status     Status
	ReasonCode string
}

// IneligibleError is a Decision that refused a candidate.
type IneligibleError struct {
	Criticality Criticality
	Decision    Decision
}

func (e *IneligibleError) Error() string {
	return fmt.Sprintf("%s health %s is not eligible for a %s capability",
		strings.ToLower(strings.ReplaceAll(string(e.Decision.Level), "_", " ")), e.Decision.Status, e.Criticality)
}

// Evaluate checks each held level separately (ADR-BCP-006 section 73): no
// level can mask another. The engine instance is always checked.
func (p *Policy) Evaluate(criticality Criticality, levels Levels, now time.Time) Decision {
	criticality = criticality.OrDefault()
	eligible := p.eligible[criticality]
	checks := []struct {
		level  Level
		obs    *Observation
		always bool
	}{
		{LevelEngineInstance, levels.EngineInstance, true},
		{LevelProvider, levels.Provider, false},
		{LevelProviderCapability, levels.ProviderCapability, false},
	}
	for _, c := range checks {
		if c.obs == nil && !c.always {
			continue
		}
		status := Effective(c.obs, now)
		if !slices.Contains(eligible, status) {
			return Decision{Level: c.level, Status: status, ReasonCode: p.resolutionDenials[status]}
		}
	}
	return Decision{Eligible: true}
}

// Check is Evaluate returning an *IneligibleError when the candidate is
// refused.
func (p *Policy) Check(criticality Criticality, levels Levels, now time.Time) error {
	d := p.Evaluate(criticality, levels, now)
	if d.Eligible {
		return nil
	}
	return &IneligibleError{Criticality: criticality.OrDefault(), Decision: d}
}

// ProvisioningBlocker is the provisioning_blocker code for a required
// capability none of whose candidates is health-eligible.
func (p *Policy) ProvisioningBlocker() string { return p.provisioningDenial }

// MarshalJSON writes reasons as an array even when there are none, as the
// contract requires.
func (o Observation) MarshalJSON() ([]byte, error) {
	type wire Observation
	w := wire(o)
	if w.Reasons == nil {
		w.Reasons = []string{}
	}
	return json.Marshal(w)
}
