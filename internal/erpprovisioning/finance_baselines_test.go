package erpprovisioning

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
)

const (
	entityA = "ZURIBEANS-ZA"
	entityB = "ZURIBEANS-UG"
)

// twoEntities is an approved plan covering two legal entities. Neither the plan nor the Control Plane says anything about
// their currencies: only ERP's Finance baseline does.
func twoEntities(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	a := approved()
	a.LegalEntityIDs = []string{entityB, entityA}
	r.w.Source = source{auth: a}
	r.erp.state["legal_entity_ids"] = []string{entityA, entityB}
	r.erp.baselines = map[string]map[string]any{
		entityA: baselineFor(entityA, "ZAR"),
		entityB: baselineFor(entityB, "UGX"),
	}
	r.erp.baselines[entityB]["reference"].(map[string]any)["baseline_id"] = "fb_01k4zuribeansug"
	return r
}

func TestSubmitSendsERPsOwnEffectiveBaselineReferencesAndCurrenciesVerbatim(t *testing.T) {
	r := twoEntities(t)
	if _, err := r.w.Submit(context.Background(), provisionID); err != nil {
		t.Fatal(err)
	}
	// One effective-baseline read per legal entity, under the provisioner's token and the same context the request names.
	if len(r.erp.baselineCalls) != 2 {
		t.Fatalf("calls=%v", r.erp.baselineCalls)
	}
	cx := r.ctx.created[0]
	// Legal entity order is the sorted order: UG before ZA.
	for i, entityID := range []string{entityB, entityA} {
		q, _, _ := strings.Cut(r.erp.baselineCalls[i], "|")
		if !strings.HasPrefix(q, entityID+"?") || !strings.HasSuffix(r.erp.baselineCalls[i], "|Bearer provisioner-token") {
			t.Fatalf("call %d: %s", i, r.erp.baselineCalls[i])
		}
		v, _ := url.ParseQuery(strings.TrimPrefix(strings.SplitN(q, "?", 2)[1], ""))
		if v.Get("context_id") != cx.ID {
			t.Fatalf("the read must carry the provisioning context: %v", v)
		}
	}
	body := r.erp.bodies[0]
	refs := body["finance_baselines"].([]any)
	if len(refs) != 2 {
		t.Fatalf("one reference per legal entity: %v", refs)
	}
	for i, want := range []struct{ entity, id string }{{entityB, "fb_01k4zuribeansug"}, {entityA, "fb_01k4zuribeansza"}} {
		ref := refs[i].(map[string]any)
		if ref["legal_entity_id"] != want.entity || ref["baseline_id"] != want.id || ref["version"] != float64(3) ||
			ref["digest"] != "sha256:"+strings.Repeat("9f", 32) {
			t.Fatalf("reference %d = %v", i, ref)
		}
	}
	// The currencies are ERP's, verbatim, as a set: nothing derived, defaulted or inferred by the Control Plane.
	var currencies []string
	for _, c := range body["functional_currencies"].([]any) {
		currencies = append(currencies, c.(string))
	}
	if !slices.Equal(currencies, []string{"UGX", "ZAR"}) {
		t.Fatalf("currencies=%v", currencies)
	}
	// The complete reference set is recorded with the submission and the immutable plan tuple.
	sub := r.led.subs[operationID]
	if len(sub.FinanceBaselines) != 2 || sub.FinanceBaselines[0].LegalEntityID != entityB || sub.FinanceBaselines[1].LegalEntityID != entityA ||
		sub.Authority.PlanDigest != planDigest {
		t.Fatalf("submission=%+v", sub)
	}
}

func TestSharedCurrenciesAreSentOnce(t *testing.T) {
	r := twoEntities(t)
	r.erp.baselines[entityB]["functional_currency"] = "ZAR"
	if _, err := r.w.Submit(context.Background(), provisionID); err != nil {
		t.Fatal(err)
	}
	if got := r.erp.bodies[0]["functional_currencies"].([]any); len(got) != 1 || got[0] != "ZAR" {
		t.Fatalf("erp/v1 declares the list unique: %v", got)
	}
}

func TestNothingIsSentWhenTheBaselineCannotBeResolved(t *testing.T) {
	type refusal struct {
		name    string
		arrange func(*rig)
		is      error
		retry   bool
	}
	for _, c := range []refusal{
		{"no approved baseline is in force", func(r *rig) { r.erp.baselineStatus, r.erp.baselineProblem = 404, "ERP_RESOURCE_NOT_FOUND" }, ErrNoFinanceBaseline, false},
		{"ERP is unavailable", func(r *rig) { r.erp.baselineStatus, r.erp.baselineProblem = 503, "ERP_SERVICE_UNAVAILABLE" }, nil, true},
		{"the context is rejected", func(r *rig) { r.erp.baselineStatus, r.erp.baselineProblem = 403, "ERP_CONTEXT_REJECTED" }, nil, false},
		{"an answer outside erp/v1", func(r *rig) { r.erp.baselines[entityA]["functional_currency"] = "zar" }, ErrFinanceBaselineUnavailable, false},
		{"an answer for another legal entity", func(r *rig) {
			r.erp.baselines[entityA]["reference"].(map[string]any)["legal_entity_id"] = "OTHER-ZA"
		}, ErrFinanceBaselineUnavailable, false},
		{"a baseline ERP does not own", func(r *rig) {
			r.erp.baselines[entityA]["reference"].(map[string]any)["authority"] = map[string]any{"engine_id": "baobab-cp", "system_of_record": "FINANCE_BASELINE"}
		}, ErrFinanceBaselineUnavailable, false},
		{"a version that is not in force", func(r *rig) { r.erp.baselines[entityA]["status"] = "SUPERSEDED" }, ErrFinanceBaselineNotUsable, false},
		{"a withdrawn version", func(r *rig) { r.erp.baselines[entityB]["status"] = "WITHDRAWN" }, ErrFinanceBaselineNotUsable, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := twoEntities(t)
			c.arrange(r)
			_, err := r.w.Submit(context.Background(), provisionID)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Fatalf("err=%v", err)
			}
			if Retryable(err) != c.retry {
				t.Fatalf("retryable=%v err=%v", Retryable(err), err)
			}
			if len(r.erp.posts) != 0 || len(r.led.subs) != 0 {
				t.Fatal("no provisioning is requested and nothing is recorded when the baseline cannot be resolved")
			}
			if strings.Contains(err.Error(), "private detail") {
				t.Fatal("ERP free text must not propagate")
			}
		})
	}
}

func TestADuplicateLegalEntityInThePlanIsRefusedBeforeAnythingIsAsked(t *testing.T) {
	r := twoEntities(t)
	a := approved()
	a.LegalEntityIDs = []string{entityA, entityA}
	r.w.Source = source{auth: a}
	if _, err := r.w.Submit(context.Background(), provisionID); !errors.Is(err, ErrFinanceBaselineUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if len(r.erp.posts) != 0 {
		t.Fatal("nothing is sent")
	}
}

func TestERPsFinanceRefusalsStayDistinctFromTransportAndFromEachOther(t *testing.T) {
	for _, c := range []struct {
		code string
		is   error
	}{
		{"FINANCE_BASELINE_MISMATCH", ErrFinanceBaselineMismatch},
		{"FINANCE_BASELINE_NOT_USABLE", ErrFinanceBaselineNotUsable},
		{"PLAN_AUTHORITY_MISMATCH", ErrPlanAuthorityMismatch},
	} {
		r := twoEntities(t)
		r.erp.status, r.erp.problem = 409, c.code
		_, err := r.w.Submit(context.Background(), provisionID)
		if !errors.Is(err, c.is) || Retryable(err) {
			t.Fatalf("%s: err=%v retryable=%v", c.code, err, Retryable(err))
		}
		for _, other := range []error{ErrFinanceBaselineMismatch, ErrFinanceBaselineNotUsable, ErrPlanAuthorityMismatch} {
			if other != c.is && errors.Is(err, other) {
				t.Fatalf("%s collapsed into %v", c.code, other)
			}
		}
		if len(r.led.subs) != 0 {
			t.Fatal("a refused request is never recorded as submitted")
		}
	}
}

func TestTheBaselineReferenceSetIsPartOfTheSubmissionIdentity(t *testing.T) {
	ref := func(entityID string, version int, digest string) FinanceBaselineReference {
		r := FinanceBaselineReference{BaselineID: "fb_" + entityID[:3], LegalEntityID: entityID, Version: version, Digest: digest, EffectiveFrom: "2026-04-01"}
		r.Authority.EngineID, r.Authority.SystemOfRecord = "baobab-erp", "FINANCE_BASELINE"
		return r
	}
	d1, d2 := "sha256:"+strings.Repeat("a1", 32), "sha256:"+strings.Repeat("b2", 32)
	base := Request{TenantID: tenant, Authority: approved().Authority, LegalEntityIDs: []string{"A-ZA", "B-ZA"},
		FinanceBaselines: []FinanceBaselineReference{ref("A-ZA", 1, d1), ref("B-ZA", 1, d1)}}
	reordered := base
	reordered.FinanceBaselines = []FinanceBaselineReference{ref("B-ZA", 1, d1), ref("A-ZA", 1, d1)}
	if IdempotencyKey(base) != IdempotencyKey(reordered) {
		t.Fatal("the same set in another order is the same submission")
	}
	for name, altered := range map[string][]FinanceBaselineReference{
		"another version": {ref("A-ZA", 2, d1), ref("B-ZA", 1, d1)},
		"another digest":  {ref("A-ZA", 1, d2), ref("B-ZA", 1, d1)},
		"a missing entry": {ref("A-ZA", 1, d1)},
	} {
		other := base
		other.FinanceBaselines = altered
		if IdempotencyKey(other) == IdempotencyKey(base) {
			t.Fatalf("%s must not silently reuse a prior submission", name)
		}
	}
}

func TestResolutionIsAskedOnlyThroughTheEffectiveRead(t *testing.T) {
	// The effective read gives the reference the Control Plane relies on; ERP proves it exact when it provisions. A second
	// getFinanceBaseline call would only make the Control Plane another Finance authority.
	r := twoEntities(t)
	if _, err := r.w.Submit(context.Background(), provisionID); err != nil {
		t.Fatal(err)
	}
	for _, call := range r.erp.baselineCalls {
		if strings.Contains(call, "digest=") || strings.Contains(call, "version=") {
			t.Fatalf("an exact-version read was made: %s", call)
		}
	}
}
