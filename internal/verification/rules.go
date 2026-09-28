package verification

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

// Machines of evidence/v1 lifecycle.yaml.
const (
	MachineEvidence    = "evidence_record"
	MachineClaim       = "evidence_claim"
	MachineCase        = "verification_case"
	MachineDiscrepancy = "evidence_discrepancy"
)

// Actors of lifecycle.yaml.
const (
	ActorReviewer  = "REVIEWER"
	ActorPlatform  = "PLATFORM"
	ActorApplicant = "APPLICANT"
)

// Case statuses and claim statuses this package moves between.
const (
	CaseVerifying   = "VERIFYING"
	CaseConflicted  = "CONFLICTED"
	CaseVerified    = "VERIFIED"
	ClaimUnderCheck = "UNDER_VERIFICATION"
	ClaimVerified   = "VERIFIED"
	EvidenceAvail   = "AVAILABLE"
)

var (
	// ErrTransition is a command the lifecycle does not allow from a
	// status, or not for the actor.
	ErrTransition = errors.New("transition not allowed")
	// ErrSelfVerification: nobody checks or decides a claim they asserted
	// (section 169).
	ErrSelfVerification = errors.New("a claim is never checked or decided by its asserter")
	// ErrUnsupported is a check or result the evidence does not support.
	ErrUnsupported = errors.New("not supported by the evidence")
	// ErrUnknownSource names a source the registry does not list, or one
	// that is not active.
	ErrUnknownSource = errors.New("unknown or inactive evidence source")
)

type transition struct {
	Command string   `yaml:"command"`
	From    string   `yaml:"from"`
	To      string   `yaml:"to"`
	Actors  []string `yaml:"actors"`
}

type machine struct {
	Initial           string            `yaml:"initial"`
	InitialByOrigin   map[string]string `yaml:"initial_by_origin"`
	Terminal          []string          `yaml:"terminal"`
	ContentAccessible []string          `yaml:"content_accessible"`
	Transitions       []transition      `yaml:"transitions"`
}

type document struct {
	Machines map[string]machine `yaml:"machines"`
	Sources  []Source
}

var (
	loadOnce sync.Once
	loaded   *document
	loadErr  error
)

func load() (*document, error) {
	loadOnce.Do(func() {
		var doc document
		raw, err := contracts.ReadEmbedded("evidence/v1/lifecycle.yaml")
		if err == nil {
			err = yaml.Unmarshal(raw, &doc)
		}
		if err != nil {
			loadErr = fmt.Errorf("load evidence/v1 lifecycle.yaml: %w", err)
			return
		}
		var registry struct {
			Sources []Source `yaml:"sources"`
		}
		raw, err = contracts.ReadEmbedded("evidence/v1/source-registry.yaml")
		if err == nil {
			err = yaml.Unmarshal(raw, &registry)
		}
		if err != nil {
			loadErr = fmt.Errorf("load evidence/v1 source-registry.yaml: %w", err)
			return
		}
		for _, name := range []string{MachineEvidence, MachineClaim, MachineCase, MachineDiscrepancy} {
			if len(doc.Machines[name].Transitions) == 0 {
				loadErr = fmt.Errorf("evidence/v1 lifecycle.yaml has no %s machine", name)
				return
			}
		}
		doc.Sources = registry.Sources
		loaded = &doc
	})
	return loaded, loadErr
}

func mustLoad() *document {
	doc, err := load()
	if err != nil {
		panic(err)
	}
	return doc
}

// Next is the status command leads to from status in machine, when actor
// may issue it.
func Next(machineName, status, command, actor string) (string, error) {
	for _, t := range mustLoad().Machines[machineName].Transitions {
		if t.From == status && t.Command == command {
			if !slices.Contains(t.Actors, actor) {
				return "", fmt.Errorf("%w: a %s may not %s", ErrTransition, actor, command)
			}
			return t.To, nil
		}
	}
	return "", fmt.Errorf("%w: %s is not allowed from %s", ErrTransition, command, status)
}

// NextTo finds the command that moves machine from status to target for
// actor (record_result names its target by the result's outcome).
func NextTo(machineName, status, command, target, actor string) error {
	for _, t := range mustLoad().Machines[machineName].Transitions {
		if t.From == status && t.Command == command && t.To == target {
			if !slices.Contains(t.Actors, actor) {
				return fmt.Errorf("%w: a %s may not %s", ErrTransition, actor, command)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %s from %s to %s", ErrTransition, command, status, target)
}

// Terminal reports whether status ends machine.
func Terminal(machineName, status string) bool {
	return slices.Contains(mustLoad().Machines[machineName].Terminal, status)
}

// ContentAccessible reports whether evidence in status may be opened by
// reviewers (section 238).
func ContentAccessible(status string) bool {
	return slices.Contains(mustLoad().Machines[MachineEvidence].ContentAccessible, status)
}

// InitialClaimStatus is the status a claim of origin starts in.
func InitialClaimStatus(origin string) string {
	return mustLoad().Machines[MachineClaim].InitialByOrigin[origin]
}

// Sources are the registered evidence sources.
func Sources() []Source { return slices.Clone(mustLoad().Sources) }

// SourceByID is a registered, active source.
func SourceByID(id string) (Source, error) {
	for _, s := range mustLoad().Sources {
		if s.SourceID == id {
			if s.Status != "ACTIVE" {
				return s, fmt.Errorf("%w: %s is %s", ErrUnknownSource, id, s.Status)
			}
			return s, nil
		}
	}
	return Source{}, fmt.Errorf("%w: %s", ErrUnknownSource, id)
}

// CaseOpen reports whether claims, checks and discrepancies may still be
// recorded on a case: it has neither ended nor concluded.
func CaseOpen(status string) bool {
	return !Terminal(MachineCase, status) && status != CaseVerified
}

// positive outcomes of a check.
var positive = []string{"MATCHED", "VERIFIED", "PARTIALLY_VERIFIED"}

// ValidateCheck applies the rules no schema can state to a check about to
// be recorded: the checker never asserted the claim, the source is
// registered and active, and a positive check cites only evidence
// reviewers may open (section 238). evidence maps each cited id to its
// record.
func ValidateCheck(claim Claim, performer string, rec CheckRecord, evidence map[string]Evidence) error {
	if performer == claim.AssertedBy {
		return ErrSelfVerification
	}
	if _, err := SourceByID(rec.SourceID); err != nil {
		return err
	}
	for _, id := range rec.EvidenceIDs {
		e, ok := evidence[id]
		if !ok {
			return fmt.Errorf("%w: evidence %s does not exist", ErrUnsupported, id)
		}
		// Evidence supports a check only about its own subject, for its own
		// purpose, from the source the check consulted.
		if e.Subject != claim.Subject || e.Purpose != claim.Purpose || e.SourceID != rec.SourceID {
			return fmt.Errorf("%w: evidence %s concerns another subject, purpose or source than this check", ErrUnsupported, id)
		}
		if slices.Contains(positive, rec.Outcome) && !ContentAccessible(e.Status) {
			return fmt.Errorf("%w: evidence %s is %s and not open to reviewers", ErrUnsupported, id, e.Status)
		}
	}
	return nil
}

// ValidateResult applies the evidence-chain rules to a result about to be
// recorded: the decider never asserted the claim; every cited check is on
// this claim in this case; VERIFIED rests on a positive check from a source
// trusted for the claim's type in its jurisdiction; and every cited
// discrepancy concerns this claim's type and subject in this case.
func ValidateResult(claim Claim, caseID, decider string, rec ResultRecord, checks map[string]Check, discrepancies map[string]Discrepancy) error {
	if decider == claim.AssertedBy {
		return ErrSelfVerification
	}
	supported := false
	for _, id := range rec.CheckIDs {
		c, ok := checks[id]
		if !ok || c.ClaimID != claim.ClaimID || c.CaseID != caseID {
			return fmt.Errorf("%w: check %s is not a check of claim %s in this case", ErrUnsupported, id, claim.ClaimID)
		}
		if c.Outcome == "VERIFIED" || c.Outcome == "MATCHED" {
			if s, err := SourceByID(c.SourceID); err == nil && s.Trusts(claim.ClaimType, claim.Jurisdiction) {
				supported = true
			}
		}
	}
	if rec.Outcome == "VERIFIED" && !supported {
		return fmt.Errorf("%w: VERIFIED needs a positive check from a source trusted for %s in %s",
			ErrUnsupported, claim.ClaimType, orAny(claim.Jurisdiction))
	}
	for _, id := range rec.DiscrepancyIDs {
		d, ok := discrepancies[id]
		if !ok || d.ClaimType != claim.ClaimType || d.Subject != claim.Subject || d.CaseID != caseID {
			return fmt.Errorf("%w: discrepancy %s is not about this claim in this case", ErrUnsupported, id)
		}
	}
	return nil
}

// ValidateDiscrepancy: every value names a registered source, and at least
// two sources disagree.
func ValidateDiscrepancy(rec DiscrepancyRecord) error {
	distinct := map[string]bool{}
	for _, v := range rec.ConflictingValues {
		if _, err := SourceByID(v.SourceID); err != nil {
			return err
		}
		distinct[v.SourceID] = true
	}
	if len(distinct) < 2 {
		return fmt.Errorf("%w: a discrepancy is between at least two sources", ErrUnsupported)
	}
	return nil
}

// ClaimStanding is the claim status a recorded result's outcome gives: the
// outcome itself, since a recorded result is always a standing.
func ClaimStanding(outcome string) string { return outcome }

func orAny(j string) string {
	if j == "" {
		return "any jurisdiction"
	}
	return j
}
