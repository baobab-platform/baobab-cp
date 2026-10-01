package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// adminRoutePermissions names the AdministrativePermission each role-guarded
// route performs, by method and chi route pattern (ADR-BCP-020 section 143:
// map each existing role to explicit permissions). Every route names a
// permission registered in Shared; a route mapped to "" would be counted as
// unregistered, never guessed, and TestEveryGuardedRouteIsMapped refuses it.
// That test keeps this table and the router in step.
var adminRoutePermissions = map[string]string{
	"POST /v1/tenants":                                                              "tenant.provision",
	"POST /v1/tenants/bootstrap-registrations":                                      "tenant.provision",
	"GET /v1/tenants/{tenantID}":                                                    "tenant.view",
	"POST /v1/tenants/{tenantID}/suspend":                                           "tenant.suspend",
	"POST /v1/tenants/{tenantID}/activate":                                          "tenant.activate",
	"POST /v1/tenants/{tenantID}/decommission":                                      "tenant.decommission",
	"GET /v1/entitlements":                                                          "subscription.view",
	"GET /v1/admin/grants":                                                          "administrator.view",
	"POST /v1/admin/grants":                                                         "administrator.grant",
	"GET /v1/admin/grants/{grantID}":                                                "administrator.view",
	"POST /v1/admin/grants/{grantID}/replacements":                                  "administrator.grant",
	"POST /v1/admin/grants/{grantID}/transitions":                                   "administrator.revoke",
	"POST /v1/capabilities/explain":                                                 "support.diagnostics.view",
	"POST /v1/canonical-entities":                                                   "organisation.manage",
	"GET /v1/canonical-entities/{entityID}":                                         "organisation.view",
	"POST /v1/canonical-entities/{entityID}/validate":                               "organisation.manage",
	"POST /v1/canonical-entities/{entityID}/activate":                               "organisation.manage",
	"POST /v1/canonical-entities/{entityID}/suspend":                                "organisation.manage",
	"POST /v1/canonical-entities/{entityID}/retire":                                 "organisation.manage",
	"POST /v1/canonical-entities/{entityID}/iam-organisations":                      "organisation.manage",
	"GET /v1/canonical-entities/{entityID}/iam-organisations":                       "organisation.view",
	"POST /v1/iam-organisation-references/{referenceID}/retire":                     "organisation.manage",
	"GET /v1/iam-organisations":                                                     "organisation.view",
	"POST /v1/tenants/{tenantID}/organisation-admission":                            "organisation.manage",
	"POST /v1/tenants/{tenantID}/counterparty-roles":                                "organisation.relationship.manage",
	"GET /v1/tenants/{tenantID}/counterparty-roles":                                 "organisation.view",
	"POST /v1/counterparty-roles/{roleID}/end":                                      "organisation.relationship.manage",
	"POST /v1/organisation-reconciliation":                                          "organisation.manage",
	"GET /v1/organisation-resolution-candidates":                                    "organisation.view",
	"GET /v1/organisation-resolution-candidates/{candidateID}":                      "organisation.view",
	"POST /v1/organisation-resolution-candidates/{candidateID}/decision":            "organisation.manage",
	"GET /v1/platform-accounts/{accountID}":                                         "platform-account.view",
	"POST /v1/platform-accounts/{accountID}/status":                                 "platform-account.manage",
	"POST /v1/tenants/{tenantID}/platform-account-binding":                          "platform-account.manage",
	"POST /v1/tenants/{tenantID}/platform-account-binding/end":                      "platform-account.manage",
	"GET /v1/tenants/{tenantID}/platform-account-bindings":                          "platform-account.view",
	"GET /v1/organisation-drift":                                                    "organisation.view",
	"GET /v1/organisations/{organisationID}/audit":                                  "audit.view",
	"GET /v1/admission/applications":                                                "application.review",
	"GET /v1/admission/applications/{applicationID}":                                "application.review",
	"GET /v1/admission/applications/{applicationID}/decision":                       "application.review",
	"POST /v1/admission/applications/{applicationID}/begin-validation":              "application.review",
	"POST /v1/admission/applications/{applicationID}/information-request":           "application.request-information",
	"POST /v1/admission/applications/{applicationID}/begin-review":                  "application.review",
	"POST /v1/admission/applications/{applicationID}/cancel":                        "application.review",
	"POST /v1/admission/applications/{applicationID}/decision":                      "application.decide",
	"POST /v1/tenant-onboarding-requests":                                           "tenant.provision",
	"GET /v1/tenant-onboarding-requests":                                            "tenant.view",
	"GET /v1/tenant-onboarding-requests/{requestID}":                                "tenant.view",
	"POST /v1/tenant-onboarding-requests/{requestID}/authorisation":                 "changeset.approve",
	"POST /v1/tenant-onboarding-requests/{requestID}/cancellation":                  "tenant.provision",
	"POST /v1/tenant-onboarding-requests/{requestID}/fulfilment":                    "tenant.provision",
	"POST /v1/product-subscriptions/{subscriptionID}/classification":                "subscription.manage",
	"POST /v1/product-subscriptions/{subscriptionID}/reclassification":              "subscription.manage",
	"GET /v1/product-subscriptions/{subscriptionID}/classification":                 "subscription.view",
	"GET /v1/tenants/{tenantID}/products/{productID}/classification":                "subscription.view",
	"POST /v1/tenants/{tenantID}/provisioning":                                      "tenant.provision",
	"GET /v1/tenants/{tenantID}/provisioning":                                       "tenant.view",
	"GET /v1/tenants/{tenantID}/provisioning/{provisioningID}":                      "tenant.view",
	"GET /v1/tenants/{tenantID}/provisioning/{provisioningID}/plan":                 "tenant.view",
	"POST /v1/tenants/{tenantID}/provisioning/{provisioningID}/plan":                "tenant.provision",
	"POST /v1/tenants/{tenantID}/provisioning/{provisioningID}/approve":             "changeset.approve",
	"POST /v1/tenants/{tenantID}/provisioning/{provisioningID}/apply":               "changeset.apply",
	"POST /v1/tenants/{tenantID}/provisioning/{provisioningID}/withdraw":            "tenant.provision",
	"POST /v1/tenants/{tenantID}/provisioning/{provisioningID}/remediate":           "reconciliation.request",
	"GET /v1/tenants/{tenantID}/provisioning/{provisioningID}/readiness":            "readiness.view",
	"GET /v1/tenants/{tenantID}/provisioning/{provisioningID}/drift":                "readiness.view",
	"POST /v1/admin/verification-cases":                                             "verification.review",
	"GET /v1/admin/verification-cases":                                              "verification.view",
	"GET /v1/admin/verification-cases/{caseID}":                                     "verification.view",
	"POST /v1/admin/verification-cases/{caseID}/transitions":                        "verification.review",
	"POST /v1/admin/verification-cases/{caseID}/claims":                             "verification.review",
	"GET /v1/admin/verification-cases/{caseID}/claims":                              "verification.view",
	"POST /v1/admin/verification-cases/{caseID}/claims/{claimID}/open-verification": "verification.review",
	"POST /v1/admin/verification-cases/{caseID}/checks":                             "verification.review",
	"GET /v1/admin/verification-cases/{caseID}/checks":                              "verification.view",
	"POST /v1/admin/verification-cases/{caseID}/results":                            "verification.decide",
	"GET /v1/admin/verification-cases/{caseID}/results":                             "verification.view",
	"POST /v1/admin/verification-cases/{caseID}/discrepancies":                      "verification.review",
	"GET /v1/admin/verification-cases/{caseID}/discrepancies":                       "verification.view",
	"POST /v1/admin/evidence-discrepancies/{discrepancyID}/transitions":             "verification.review",
	"POST /v1/admin/evidence":                                                       "evidence.register",
	"GET /v1/admin/evidence/{evidenceID}":                                           "evidence.view",
	"GET /v1/admin/evidence-sources":                                                "evidence.view",
	"POST /v1/admin/evidence-discrepancies/{discrepancyID}/resolution":              "verification.decide",
	"POST /v1/admin/verification-cases/{caseID}/conclusion":                         "verification.decide",
	"POST /v1/markets":                                           "market.request",
	"GET /v1/markets/{marketID}":                                 "market.view",
	"PATCH /v1/markets/{marketID}":                               "market.request",
	"POST /v1/markets/{marketID}/activate":                       "market.activate",
	"POST /v1/admin/changesets":                                  "changeset.submit",
	"GET /v1/admin/changesets":                                   "changeset.review",
	"GET /v1/admin/changesets/{changesetID}":                     "changeset.review",
	"POST /v1/admin/changesets/{changesetID}/submit":             "changeset.submit",
	"GET /v1/admin/changesets/{changesetID}/plan":                "changeset.review",
	"POST /v1/admin/changesets/{changesetID}/approve":            "changeset.approve",
	"POST /v1/admin/changesets/{changesetID}/apply":              "changeset.apply",
	"POST /v1/admin/changesets/{changesetID}/cancel":             "changeset.submit",
	"GET /v1/admin/changesets/{changesetID}/outcome":             "changeset.review",
	"POST /v1/provider-migrations/plan":                          "topology.view",
	"POST /v1/provider-migrations":                               "provider-migration.plan",
	"GET /v1/provider-migrations/{providerMigrationID}":          "topology.view",
	"GET /v1/provider-migrations/{providerMigrationID}/plan":     "topology.view",
	"POST /v1/provider-migrations/{providerMigrationID}/approve": "provider-migration.approve",
	"POST /v1/provider-migrations/{providerMigrationID}/advance": "provider-migration.execute",
	// ADR-BCP-025 gate ER-02: an administrator recording an engine release
	// (release tooling records under its workload scope instead).
	"POST /v1/engine-releases":                                            "engine-release.record",
	"GET /v1/engine-releases":                                             "topology.view",
	"GET /v1/engine-releases/{releaseID}":                                 "topology.view",
	"POST /v1/engine-releases/{releaseID}/status-changes":                 "engine-release.change-status",
	"GET /v1/engine-instances/{engineInstanceID}/desired-release":         "topology.view",
	"GET /v1/engine-instances/{engineInstanceID}/deployment-observations": "topology.view",
	"GET /v1/engine-instances/{engineInstanceID}/observed-release":        "topology.view",
	// Canonical mapping administration (ADR-SHARED-013): activation is the
	// four-eyes, never-delegated mapping.approve.
	"POST /v1/external-references":                      "mapping.manage",
	"GET /v1/external-references":                       "mapping.view",
	"GET /v1/external-references/{externalReferenceID}": "mapping.view",
	"POST /v1/resolution/external-references":           "mapping.view",
	"POST /v1/mappings":                                 "mapping.manage",
	"GET /v1/mappings/{mappingID}":                      "mapping.view",
	"PATCH /v1/mappings/{mappingID}":                    "mapping.manage",
	"POST /v1/mappings/{mappingID}/validate":            "mapping.manage",
	"POST /v1/mappings/{mappingID}/activate":            "mapping.approve",
	"POST /v1/mappings/{mappingID}/retire":              "mapping.manage",
}

// shadowBudget bounds how long shadow evaluation may add to a request. On
// timeout the comparison is counted as an error; the response never waits
// longer and never changes.
const shadowBudget = 250 * time.Millisecond

// shadowAdministrativeDecision evaluates the request against the caller's
// AdministrativeGrants beside the legacy role decision and records how they
// compare (ADR-BCP-020 section 144). It never alters the legacy decision.
func (a *API) shadowAdministrativeDecision(r *http.Request, principal auth.Principal, legacyAllowed bool) {
	if a.grants == nil {
		return
	}
	legacy := metrics.ShadowDeny
	if legacyAllowed {
		legacy = metrics.ShadowAllow
	}
	pattern := r.Method + " " + chi.RouteContext(r.Context()).RoutePattern()
	permission, mapped := adminRoutePermissions[pattern]
	if !mapped || permission == "" {
		a.recordShadow(metrics.ShadowUnregisteredPermission, legacy, metrics.ShadowUnmapped, "", pattern)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), shadowBudget)
	defer cancel()
	grants, comparable := a.shadowGrants(ctx, r, principal, permission)
	if !comparable {
		a.recordShadow(permission, legacy, grants, metrics.ShadowNotEvaluated, pattern)
		return
	}
	a.recordShadow(permission, legacy, grants, "", pattern)
}

// shadowGrants returns the grants outcome and whether it can be compared
// with the legacy decision. It cannot when grants do not allow and the
// caller holds a live grant of the permission at a level the resource was
// not anchored at: the route names its target only indirectly, so the grant
// may well cover it, and counting grants_narrower would be false evidence.
func (a *API) shadowGrants(ctx context.Context, r *http.Request, principal auth.Principal, permission string) (string, bool) {
	if a.identities == nil {
		return metrics.ShadowError, true
	}
	caller, err := a.identities.ResolveIdentity(ctx, principal.Issuer, principal.Subject)
	switch {
	case errors.Is(err, repository.ErrIdentityNotFound):
		return metrics.ShadowUnresolved, true
	case err != nil:
		// A store failure or the budget running out is not an absent
		// principal: count it as an error, never as a comparison.
		return metrics.ShadowError, true
	case caller.Status != "ACTIVE":
		return metrics.ShadowUnresolved, true
	}
	grants, sources, err := a.grants.AdministrativeGrantsOf(ctx, caller.ID)
	if err != nil {
		return metrics.ShadowError, true
	}
	resource, err := a.shadowResource(ctx, r)
	if err != nil {
		return metrics.ShadowError, true
	}
	now := time.Now().UTC()
	// An organisation grant reaches a tenant through its effective
	// TenantOrganisationMapping and no other way (ADR-BCP-018 section 50);
	// the same relation judges a delegation against its source.
	scopes := []administration.Scope{{TenantID: resource.TenantID}}
	for _, g := range grants {
		scopes = append(scopes, g.Scope)
	}
	for _, g := range sources {
		scopes = append(scopes, g.Scope)
	}
	rel, err := a.grants.EffectiveRelations(ctx, administration.TenantsOf(scopes...), now)
	if err != nil {
		return metrics.ShadowError, true
	}
	resource = rel.ResolveResource(resource)
	decision := administration.Evaluate(administration.Request{
		PrincipalID: caller.ID, PrincipalActive: true, Action: permission, Resource: resource, Relations: rel,
		// The assurance the verified token asserts: a grant whose risk class
		// or condition needs more counts as step_up (section 72).
		Session: administration.Session{ACR: principal.Assurance.ACR, AMR: principal.Assurance.AMR, AuthenticatedAt: principal.Assurance.AuthenticatedAt},
		Now:     now, Grants: grants, Sources: sources,
	})
	outcome := metrics.ShadowDeny
	switch decision.Outcome {
	case administration.OutcomeAllow:
		return metrics.ShadowAllow, true
	case administration.OutcomeStepUpRequired:
		outcome = metrics.ShadowStepUp
	case administration.OutcomeApprovalRequired:
		outcome = metrics.ShadowApproval
	case administration.OutcomeNotReady:
		outcome = metrics.ShadowNotReady
	}
	for _, g := range grants {
		if g.Permission == permission && g.Status == administration.StatusActive && g.ValidAt(now) && !resource.Anchors(g.Scope.Level) {
			return outcome, false
		}
	}
	return outcome, true
}

// shadowResource is the resource a route acts on: identifiers from its own
// path and query, and for the tenant platform-account binding routes the
// account from the request body or the active binding. A route naming none
// is platform-level, which only a PLATFORM grant covers.
func (a *API) shadowResource(ctx context.Context, r *http.Request) (administration.Resource, error) {
	res := administration.Resource{Environment: a.environment}
	res.TenantID = chi.URLParam(r, "tenantID")
	if res.TenantID == "" {
		res.TenantID = r.URL.Query().Get("tenantId")
	}
	res.OrganisationID = chi.URLParam(r, "organisationID")
	if res.OrganisationID == "" {
		res.OrganisationID = chi.URLParam(r, "entityID")
	}
	res.PlatformAccountID = chi.URLParam(r, "accountID")
	res.MarketID = chi.URLParam(r, "marketID")
	if caseID := chi.URLParam(r, "caseID"); caseID != "" && a.verification != nil {
		if err := a.anchorCase(ctx, &res, caseID); err != nil {
			return res, err
		}
	}
	switch r.Method + " " + chi.RouteContext(r.Context()).RoutePattern() {
	case "POST /v1/markets":
		res.TenantID = peekBodyField(r, "owner_tenant_id")
	case "POST /v1/admin/verification-cases":
		res.OrganisationID = peekBodyField(r, "organisation_id")
	case "POST /v1/admin/evidence-discrepancies/{discrepancyID}/transitions",
		"POST /v1/admin/evidence-discrepancies/{discrepancyID}/resolution":
		// A discrepancy is anchored by its case's organisation.
		if a.verification == nil {
			break
		}
		caseID, err := a.verification.DiscrepancyCase(ctx, chi.URLParam(r, "discrepancyID"))
		if errors.Is(err, repository.ErrVerificationNotFound) {
			break
		}
		if err != nil {
			return res, err
		}
		if err := a.anchorCase(ctx, &res, caseID); err != nil {
			return res, err
		}
	case "GET /v1/markets/{marketID}", "PATCH /v1/markets/{marketID}", "POST /v1/markets/{marketID}/activate":
		// A market is anchored by its id and its owner tenant.
		if a.markets == nil {
			break
		}
		m, err := a.markets.GetRegistryMarket(ctx, res.MarketID)
		if errors.Is(err, repository.ErrRegistryMarketNotFound) {
			break
		}
		if err != nil {
			return res, err
		}
		res.TenantID = m.String("owner_tenant_id")
	case "POST /v1/tenants/{tenantID}/platform-account-binding":
		res.PlatformAccountID = peekPlatformAccountID(r)
	case "POST /v1/tenants/{tenantID}/platform-account-binding/end":
		if a.platformAccounts == nil {
			break
		}
		bindings, err := a.platformAccounts.ListTenantPlatformAccountBindings(ctx, res.TenantID)
		if err != nil {
			return res, err
		}
		for _, b := range bindings {
			if b.Status == "ACTIVE" {
				res.PlatformAccountID = b.PlatformAccountID
				break
			}
		}
	}
	return res, nil
}

// anchorCase anchors a verification case's routes by the organisation it
// concerns, when it names one.
func (a *API) anchorCase(ctx context.Context, res *administration.Resource, caseID string) error {
	c, err := a.verification.GetVerificationCase(ctx, caseID)
	if errors.Is(err, repository.ErrVerificationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	res.OrganisationID = c.OrganisationID
	return nil
}

// peekPlatformAccountID reads platform_account_id from a binding request
// body; see peekBodyField.
func peekPlatformAccountID(r *http.Request) string { return peekBodyField(r, "platform_account_id") }

// peekBodyField reads one top-level string field from a JSON request body
// and restores the body unchanged for the handler, which validates it. An
// unreadable or malformed body leaves the field unanchored.
func peekBodyField(r *http.Request, field string) string {
	if r.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(raw), r.Body), r.Body}
	if err != nil {
		return ""
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	var value string
	if json.Unmarshal(body[field], &value) != nil {
		return ""
	}
	return value
}

// recordShadow counts one comparison. agreement is computed from legacy
// and grants unless the caller already knows it (not_evaluated).
func (a *API) recordShadow(permission, legacy, grants, agreement, pattern string) {
	grantsAllow := grants == metrics.ShadowAllow
	switch {
	case agreement != "":
	case grants == metrics.ShadowUnmapped, grants == metrics.ShadowError:
		agreement = metrics.ShadowNotEvaluated
	case grantsAllow == (legacy == metrics.ShadowAllow):
		agreement = metrics.ShadowAgree
	case grantsAllow:
		agreement = metrics.ShadowGrantsBroader
	default:
		agreement = metrics.ShadowGrantsNarrower
	}
	metrics.AdministrativeAuthorityShadow.Add(1, permission, legacy, grants, agreement)
	if agreement == metrics.ShadowGrantsBroader {
		// Never expected: grants must be equal to or narrower than the
		// roles they replace. Loud, but the response is unchanged.
		slog.Warn("administrative grants would allow what the role check denied", "route", pattern, "permission", permission)
	}
}
