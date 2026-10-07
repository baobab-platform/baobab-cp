package erpprovisioning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
)

var (
	// ErrNotAuthorised means the provisioning has no approved, current plan to
	// execute. Nothing was sent to ERP.
	ErrNotAuthorised = errors.New("the provisioning has no approved plan to execute")
	// ErrPlanAuthorityMismatch is ERP's 409: the approved plan the request names
	// is not what Control Plane's assignment says. Replan; do not retry.
	ErrPlanAuthorityMismatch = errors.New("ERP found the request disagrees with the approved plan")
	// ErrUnknownOperation is a provisioning.changed event for an operation this
	// Control Plane never submitted; it is not ours to act on.
	ErrUnknownOperation = errors.New("the ERP operation was not submitted by this control plane")
	// ErrStateDisagrees is an ERP answer whose tenant or legal entities differ
	// from what was submitted. It is never recorded.
	ErrStateDisagrees = errors.New("the ERP state disagrees with the submitted request")
	// ErrNoFinanceBaseline means ERP has no approved Finance baseline in force for a legal entity of the provisioning (or
	// does not know the entity there). Nothing was sent: Finance approves a baseline in ERP first, and the Control Plane
	// never substitutes one.
	ErrNoFinanceBaseline = errors.New("ERP has no approved Finance baseline in force for a legal entity")
	// ErrFinanceBaselineUnavailable means the baseline answers could not be trusted (an answer that does not conform to
	// erp/v1, names another entity or another owner, or repeats an entity). Nothing was sent.
	ErrFinanceBaselineUnavailable = errors.New("the Finance baseline of the legal entities could not be resolved")
	// ErrFinanceBaselineMismatch is ERP's 409 FINANCE_BASELINE_MISMATCH: ERP does not hold exactly the referenced baseline.
	// ERP provisioned nothing; resolve the reference again, do not retry the same request.
	ErrFinanceBaselineMismatch = errors.New("ERP does not hold the referenced Finance baseline")
	// ErrFinanceBaselineNotUsable is ERP's 409 FINANCE_BASELINE_NOT_USABLE: the exact baseline version is withdrawn,
	// superseded or not yet effective. ERP provisioned nothing.
	ErrFinanceBaselineNotUsable = errors.New("the referenced Finance baseline is not usable")
)

// Authorised is everything the approved plan authorises ERP to provision for one
// tenant provisioning: the tenant, the exact plan tuple, and the legal
// entities and countries it covers. Functional currencies are deliberately not
// here: the plan does not carry them, and they come from ERP's Finance baseline
// (Worker.Submit). The provisioning's Control
// Plane source (desired state, approved plan, approval) assembles it; the
// approving human is provenance there and never appears as a caller here.
type Authorised struct {
	TenantID               string
	Authority              Authority
	LegalEntityIDs         []string
	Countries              []string
	DeploymentPolicyID     string
	LocalisationProfileIDs []string
}

// Source returns the approved authority for a provisioning, or ErrNotAuthorised
// when it is not approved, was withdrawn, or its plan is stale.
type Source interface {
	Authorised(ctx context.Context, tenantProvisioningID string) (Authorised, error)
}

// Submission is what the Control Plane remembers about an ERP operation it
// requested: ERP's state carries no provisioning id, so this link is the only
// way an event or a read is tied back to a provisioning and its plan.
type Submission struct {
	TenantProvisioningID string
	TenantID             string
	Authority            Authority
	LegalEntityIDs       []string
	// FinanceBaselines is the complete set of Finance baseline references the request carried, one per legal entity, exactly
	// as submitted. With the plan tuple it lets an audit answer which Finance baseline caused this provisioning.
	FinanceBaselines []FinanceBaselineReference
	OperationID      string
	LastRevision     int64
	LastState        string
}

// Intent is what a provisioning request for an approved plan will carry from the Finance side, fixed before the request is
// first sent. Finance may supersede a baseline at any time; the intent is why a retry still sends the same request.
type Intent struct {
	TenantProvisioningID string
	Authority            Authority
	FinanceBaselines     []FinanceBaselineReference
	FunctionalCurrencies []string
}

// Ledger persists submissions and the ERP progress recorded against them.
type Ledger interface {
	// Submitted records the operation ERP accepted for a provisioning. Recording
	// the same operation again (an idempotent replay) is not an error.
	Submitted(ctx context.Context, sub Submission, st State) error
	// Lookup finds a submission by ERP operation id.
	Lookup(ctx context.Context, operationID string) (Submission, bool, error)
	// Intent returns the Finance baseline references recorded for the approved plan before its request was first sent.
	Intent(ctx context.Context, tenantProvisioningID string, authority Authority) (Intent, bool, error)
	// RecordIntent records the intent for the approved plan unless one exists, and returns the one that is recorded: the
	// first writer wins, so concurrent or repeated attempts all send the same references under the same key.
	RecordIntent(ctx context.Context, in Intent) (Intent, error)
	// DiscardIntent removes the unsubmitted intent of the approved plan after ERP definitively refused its Finance references,
	// so the next attempt resolves them again. It never removes an intent whose plan has a recorded submission.
	DiscardIntent(ctx context.Context, tenantProvisioningID string, authority Authority) error
	// ForPlan finds the submission already recorded for an approved plan tuple of a provisioning, if any. One approved plan
	// has at most one ERP operation, so a retry must continue that operation rather than request another.
	ForPlan(ctx context.Context, tenantProvisioningID string, authority Authority) (Submission, bool, error)
	// Apply records st for the operation only if its revision is newer than the
	// recorded one, and reports whether it was newer. Events arrive late and out
	// of order, and a read can race an event; an older state never overwrites a
	// newer one.
	Apply(ctx context.Context, operationID string, st State) (bool, error)
}

// Worker requests ERP provisioning for approved plans and records what ERP
// reports. It is idempotent end to end: a repeated Submit returns ERP's prior
// operation, and a repeated or stale state is ignored.
type Worker struct {
	Source  Source
	Client  *Client
	Context ContextIssuer
	Ledger  Ledger
}

// Submit requests provisioning for one approved provisioning. The ERP answer
// means "accepted", never "provisioned": progress arrives as
// provisioning.changed events (OnProvisioningChanged), with Reconcile as the
// recovery path.
func (w Worker) Submit(ctx context.Context, tenantProvisioningID string) (State, error) {
	auth, err := w.Source.Authorised(ctx, tenantProvisioningID)
	if err != nil {
		return State{}, err
	}
	if auth.Authority.TenantProvisioningID != tenantProvisioningID {
		return State{}, fmt.Errorf("%w: the authority names another provisioning", ErrNotAuthorised)
	}
	// An approved plan has one ERP operation. If it is already recorded, a retry (an orchestrator that failed after the
	// submission, a redelivered trigger) continues that operation: the Finance baseline in force may have moved on since,
	// and asking again would send different references under a different idempotency key.
	if prior, found, err := w.Ledger.ForPlan(ctx, tenantProvisioningID, auth.Authority); err != nil {
		return State{}, err
	} else if found {
		st, _, err := w.Reconcile(ctx, prior.OperationID)
		return st, err
	}
	// The Context's tenant is the provisioning's tenant, never a caller input.
	cx, err := w.Context.Issue(ctx, auth.TenantID, auth.Authority, tenantProvisioningID)
	if err != nil {
		return State{}, err
	}
	// Finance owns the accounting facts. For every legal entity ERP names the baseline version in force and its functional
	// currency, under the same provisioning context; both are taken verbatim and nothing here derives or defaults them. The
	// provisioning command is where ERP independently proves the exact references are still usable, so they are not asked
	// for a second time. The answer is recorded as the plan's intent before anything is sent: a retry after a crash then
	// sends the same references under the same key, even if Finance has moved on, and ERP returns its prior operation.
	intent, found, err := w.Ledger.Intent(ctx, tenantProvisioningID, auth.Authority)
	if err != nil {
		return State{}, err
	}
	if !found {
		refs, currencies, err := w.resolveBaselines(ctx, cx.ID, auth.LegalEntityIDs, tenantProvisioningID)
		if err != nil {
			return State{}, err
		}
		intent, err = w.Ledger.RecordIntent(ctx, Intent{TenantProvisioningID: tenantProvisioningID, Authority: auth.Authority,
			FinanceBaselines: refs, FunctionalCurrencies: currencies})
		if err != nil {
			return State{}, fmt.Errorf("record ERP submission intent: %w", err)
		}
	}
	baselines, currencies := intent.FinanceBaselines, intent.FunctionalCurrencies
	req := Request{
		TenantID: auth.TenantID, ContextID: cx.ID, Authority: auth.Authority,
		LegalEntityIDs: auth.LegalEntityIDs, FinanceBaselines: baselines, RequestedCountries: auth.Countries, FunctionalCurrencies: currencies,
		DeploymentPolicyID: auth.DeploymentPolicyID, LocalisationProfileIDs: auth.LocalisationProfileIDs,
	}
	st, err := w.Client.Provision(ctx, req, tenantProvisioningID)
	if err != nil {
		var p *Problem
		switch {
		case errors.As(err, &p) && p.PlanAuthorityMismatch():
			return State{}, fmt.Errorf("%w", ErrPlanAuthorityMismatch)
		case errors.As(err, &p) && p.FinanceBaselineMismatch():
			return State{}, w.refused(ctx, tenantProvisioningID, auth.Authority, ErrFinanceBaselineMismatch)
		case errors.As(err, &p) && p.FinanceBaselineNotUsable():
			return State{}, w.refused(ctx, tenantProvisioningID, auth.Authority, ErrFinanceBaselineNotUsable)
		}
		return State{}, err
	}
	sub := Submission{TenantProvisioningID: tenantProvisioningID, TenantID: auth.TenantID, Authority: auth.Authority,
		LegalEntityIDs: auth.LegalEntityIDs, FinanceBaselines: baselines, OperationID: st.OperationID}
	if err := agrees(sub, st); err != nil {
		return State{}, err
	}
	if err := w.Ledger.Submitted(ctx, sub, st); err != nil {
		return State{}, fmt.Errorf("record ERP submission: %w", err)
	}
	slog.InfoContext(ctx, "erp provisioning requested", "tenant_provisioning_id", tenantProvisioningID,
		"tenant_id", auth.TenantID, "operation_id", st.OperationID, "state", st.State, "revision", st.Revision)
	return st, nil
}

// refused handles ERP's definitive refusal of the Finance references: no operation exists, so the recorded intent is dropped
// and the next attempt resolves the baseline again (Finance may have superseded it since). Ambiguous outcomes (a timeout, a
// 5xx, a lost answer) never come here: the intent stays fixed, because ERP may already have accepted the request.
func (w Worker) refused(ctx context.Context, tenantProvisioningID string, authority Authority, refusal error) error {
	if err := w.Ledger.DiscardIntent(ctx, tenantProvisioningID, authority); err != nil {
		return fmt.Errorf("%w: discard the refused intent: %v", refusal, err)
	}
	return fmt.Errorf("%w", refusal)
}

// resolveBaselines asks ERP for the Finance baseline in force for each legal entity and returns the references to send, in
// legal entity order, and the set of functional currencies ERP reported. It fails closed: an entity ERP has no baseline
// for, an answer that is not exactly one conforming, ERP-owned, effective baseline for exactly that entity, or any
// resolver failure means nothing is submitted.
func (w Worker) resolveBaselines(ctx context.Context, contextID string, legalEntityIDs []string, correlationID string) ([]FinanceBaselineReference, []string, error) {
	var refs []FinanceBaselineReference
	seen := map[string]bool{}
	var currencies []string
	for _, entity := range sorted(legalEntityIDs) {
		if seen[entity] {
			return nil, nil, fmt.Errorf("%w: legal entity %s is listed twice", ErrFinanceBaselineUnavailable, entity)
		}
		seen[entity] = true
		res, err := w.Client.EffectiveFinanceBaseline(ctx, contextID, entity, correlationID)
		if err != nil {
			var p *Problem
			if errors.As(err, &p) {
				if p.NotFound() {
					return nil, nil, fmt.Errorf("%w: %s", ErrNoFinanceBaseline, entity)
				}
				return nil, nil, err
			}
			if ctx.Err() != nil {
				return nil, nil, err
			}
			return nil, nil, fmt.Errorf("%w: %s: %v", ErrFinanceBaselineUnavailable, entity, err)
		}
		ref := res.Reference
		switch {
		case ref.LegalEntityID != entity:
			return nil, nil, fmt.Errorf("%w: ERP answered for another legal entity", ErrFinanceBaselineUnavailable)
		case ref.Authority.EngineID != "baobab-erp" || ref.Authority.SystemOfRecord != "FINANCE_BASELINE":
			return nil, nil, fmt.Errorf("%w: the baseline of %s is not owned by ERP", ErrFinanceBaselineUnavailable, entity)
		case res.Status != "EFFECTIVE":
			// The read names the version in force; anything else means it is not usable now.
			return nil, nil, fmt.Errorf("%w: %s is %s", ErrFinanceBaselineNotUsable, entity, res.Status)
		}
		refs = append(refs, ref)
		currencies = append(currencies, res.FunctionalCurrency)
	}
	if len(refs) == 0 {
		return nil, nil, fmt.Errorf("%w: no legal entity", ErrFinanceBaselineUnavailable)
	}
	return refs, uniqueStrings(currencies), nil
}

// OnProvisioningChanged handles the data of a
// com.baobab-platform.erp.provisioning.changed.v1 event: it is validated
// against erp/v1, tied to its submission, and applied only if newer.
func (w Worker) OnProvisioningChanged(ctx context.Context, data []byte) (State, bool, error) {
	st, err := ParseState(data)
	if err != nil {
		return State{}, false, err
	}
	return w.record(ctx, st)
}

// Reconcile reads an operation from ERP and records it. It exists for recovery
// when an event was missed or is overdue; callers must not poll with it.
func (w Worker) Reconcile(ctx context.Context, operationID string) (State, bool, error) {
	if _, found, err := w.Ledger.Lookup(ctx, operationID); err != nil {
		return State{}, false, err
	} else if !found {
		return State{}, false, ErrUnknownOperation
	}
	st, err := w.Client.Operation(ctx, operationID, operationID)
	if err != nil {
		return State{}, false, err
	}
	if st.OperationID != operationID {
		return State{}, false, fmt.Errorf("%w: another operation was returned", ErrStateDisagrees)
	}
	return w.record(ctx, st)
}

func (w Worker) record(ctx context.Context, st State) (State, bool, error) {
	sub, found, err := w.Ledger.Lookup(ctx, st.OperationID)
	if err != nil {
		return State{}, false, err
	}
	if !found {
		return State{}, false, ErrUnknownOperation
	}
	if err := agrees(sub, st); err != nil {
		return State{}, false, err
	}
	applied, err := w.Ledger.Apply(ctx, st.OperationID, st)
	if err != nil {
		return State{}, false, fmt.Errorf("record ERP state: %w", err)
	}
	return st, applied, nil
}

func agrees(sub Submission, st State) error {
	if st.TenantID != sub.TenantID {
		return fmt.Errorf("%w: tenant", ErrStateDisagrees)
	}
	if !slices.Equal(sorted(st.LegalEntityIDs), sorted(sub.LegalEntityIDs)) {
		return fmt.Errorf("%w: legal entities", ErrStateDisagrees)
	}
	return nil
}

// Retryable reports whether err is worth retrying later with the same
// authority: ERP or Control Plane being unavailable, not a refusal of the
// request itself.
func Retryable(err error) bool {
	var p *Problem
	if errors.As(err, &p) {
		return p.Retryable
	}
	return false
}
