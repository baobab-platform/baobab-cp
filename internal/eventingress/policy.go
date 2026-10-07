package eventingress

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

// Allowed is one event type the Control Plane consumes over signed delivery (control-plane/v1 event-ingress.yaml).
type Allowed struct {
	Type          string `yaml:"type"`
	Producer      string `yaml:"producer"`
	Source        string `yaml:"source"`
	Scope         string `yaml:"scope"`
	PendingMaxAge time.Duration
	PendingHours  int `yaml:"pending_max_age_hours"`
}

// Policy is the closed list of events the Control Plane accepts, read from the embedded Shared contract.
type Policy struct {
	Accepted      []Allowed `yaml:"accepted"`
	RetentionDays int       `yaml:"receipt_retention_days"`
	Recipient     string    `yaml:"recipient"`
	Window        int       `yaml:"replay_window_seconds"`
}

// LoadPolicy reads event-ingress.yaml and refuses a policy that disagrees with what this package implements.
func LoadPolicy() (Policy, error) {
	raw, err := contracts.ReadEmbedded("control-plane/v1/event-ingress.yaml")
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("event-ingress.yaml: %w", err)
	}
	if p.Recipient != Recipient || time.Duration(p.Window)*time.Second != ReplayWindow {
		return Policy{}, fmt.Errorf("event-ingress.yaml recipient and replay window differ from the implementation")
	}
	if len(p.Accepted) == 0 || p.RetentionDays < 7 {
		return Policy{}, fmt.Errorf("event-ingress.yaml must list accepted events and keep receipts at least 7 days")
	}
	for i := range p.Accepted {
		a := &p.Accepted[i]
		if a.Type == "" || a.Producer == "" || a.Source == "" || a.Scope != "tenant" || a.PendingHours <= 0 {
			return Policy{}, fmt.Errorf("event-ingress.yaml entry %d is incomplete", i)
		}
		a.PendingMaxAge = time.Duration(a.PendingHours) * time.Hour
	}
	return p, nil
}

// Lookup returns the accepted entry for an event type.
func (p Policy) Lookup(eventType string) (Allowed, bool) {
	for _, a := range p.Accepted {
		if a.Type == eventType {
			return a, true
		}
	}
	return Allowed{}, false
}

// Producers are the senders a delivery key may be registered for.
func (p Policy) Producers() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range p.Accepted {
		if !seen[a.Producer] {
			seen[a.Producer] = true
			out = append(out, a.Producer)
		}
	}
	return out
}

// Retention is how long receipts are kept at least.
func (p Policy) Retention() time.Duration { return time.Duration(p.RetentionDays) * 24 * time.Hour }
