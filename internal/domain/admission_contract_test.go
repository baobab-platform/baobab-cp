package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"gopkg.in/yaml.v3"
)

// TestApplicationLifecycleIsSharedLifecycle: the transition table the
// Control Plane enforces is exactly Shared's contracts/admission/v1
// lifecycle.yaml, its statuses and subscription types are the contract's
// enums, and its events are the ones asyncapi.yaml registers.
func TestApplicationLifecycleIsSharedLifecycle(t *testing.T) {
	dir := filepath.Join(contracttest.SharedDir(t), "contracts", "admission", "v1")
	var lifecycle struct {
		Initial     string   `yaml:"initial"`
		Terminal    []string `yaml:"terminal"`
		Editable    []string `yaml:"editable_by_applicant"`
		Transitions []struct {
			Command, From, To, Actor string
		} `yaml:"transitions"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &lifecycle); err != nil {
		t.Fatal(err)
	}
	var shared []domain.ApplicationTransition
	for _, tr := range lifecycle.Transitions {
		shared = append(shared, domain.ApplicationTransition{Command: domain.ApplicationCommand(tr.Command),
			From: domain.ApplicationStatus(tr.From), To: domain.ApplicationStatus(tr.To), Actor: domain.AdmissionActor(tr.Actor)})
	}
	if !slices.Equal(shared, domain.ApplicationTransitions) {
		t.Fatalf("transition table differs from Shared lifecycle.yaml:\nshared %v\nlocal  %v", shared, domain.ApplicationTransitions)
	}
	if lifecycle.Initial != string(domain.ApplicationDraft) {
		t.Fatalf("initial status %q", lifecycle.Initial)
	}

	var application, decision struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	for name, into := range map[string]any{"application.schema.json": &application, "decision.schema.json": &decision} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range application.Defs["applicationStatus"].Enum {
		s := domain.ApplicationStatus(status)
		if !s.Valid() {
			t.Errorf("status %s is not known to the Control Plane", s)
		}
		if s.Terminal() != slices.Contains(lifecycle.Terminal, status) {
			t.Errorf("status %s: terminal=%v disagrees with lifecycle.yaml", s, s.Terminal())
		}
		if s.EditableByApplicant() != slices.Contains(lifecycle.Editable, status) {
			t.Errorf("status %s: editable=%v disagrees with lifecycle.yaml", s, s.EditableByApplicant())
		}
	}
	if len(application.Defs["applicationStatus"].Enum) != 10 {
		t.Fatalf("expected the ten ADR-BCP-017 statuses, got %v", application.Defs["applicationStatus"].Enum)
	}
	want := []string{string(domain.SubscriptionCommercial), string(domain.SubscriptionInternal), string(domain.SubscriptionTrial),
		string(domain.SubscriptionPartner), string(domain.SubscriptionManual), string(domain.SubscriptionMigration)}
	if !slices.Equal(decision.Defs["subscriptionType"].Enum, want) {
		t.Fatalf("subscription types %v, want %v", decision.Defs["subscriptionType"].Enum, want)
	}

	var asyncapi struct {
		Components struct {
			Messages map[string]struct {
				Name string `yaml:"name"`
			} `yaml:"messages"`
		} `yaml:"components"`
	}
	raw, err = os.ReadFile(filepath.Join(dir, "asyncapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &asyncapi); err != nil {
		t.Fatal(err)
	}
	for key, message := range asyncapi.Components.Messages {
		if events.AdmissionPayloadDef(message.Name) != key {
			t.Errorf("event %s (%s) is not mapped to its payload $def", message.Name, key)
		}
	}
	for _, eventType := range []string{events.ClientApplicationCreated, events.ClientApplicationSubmitted,
		events.ClientApplicationInformationRequested, events.ClientApplicationWithdrawn,
		events.ClientApplicationApproved, events.ClientApplicationRejected} {
		if _, ok := asyncapi.Components.Messages[events.AdmissionPayloadDef(eventType)]; !ok {
			t.Errorf("%s is not registered in Shared", eventType)
		}
	}
}

func TestNextApplicationStatus(t *testing.T) {
	for _, tc := range []struct {
		command domain.ApplicationCommand
		from    domain.ApplicationStatus
		to      domain.ApplicationStatus
		actor   domain.AdmissionActor
		ok      bool
	}{
		{domain.CommandSubmit, domain.ApplicationDraft, domain.ApplicationSubmitted, domain.ActorApplicant, true},
		{domain.CommandApprove, domain.ApplicationUnderReview, domain.ApplicationApproved, domain.ActorDecider, true},
		{domain.CommandApprove, domain.ApplicationDraft, "", "", false},      // section 8: no jump from DRAFT
		{domain.CommandApprove, domain.ApplicationValidating, "", "", false}, // only from review
		{domain.CommandWithdraw, domain.ApplicationApproved, "", "", false},  // terminal
		{domain.CommandCancel, domain.ApplicationDraft, "", "", false},       // an unsubmitted draft is withdrawn, not cancelled
	} {
		to, actor, ok := domain.NextApplicationStatus(tc.command, tc.from)
		if to != tc.to || actor != tc.actor || ok != tc.ok {
			t.Errorf("%s from %s = (%s, %s, %v), want (%s, %s, %v)", tc.command, tc.from, to, actor, ok, tc.to, tc.actor, tc.ok)
		}
	}
}
