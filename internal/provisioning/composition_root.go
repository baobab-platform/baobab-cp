// ZB-02 composition root. This file replaces the design-note stubs that
// used to live under /integration (composition_root.patch.go,
// orchestrator.patch.go, readiness_probes.go.patch.go,
// orchestrator-repair-loop.patch.go) with real, wired, tested Go code: it
// builds one concrete Orchestrator per resolved TenantManifest by
// composing ApplyStep/ResourceReconciler/ResourceReadinessProbe
// implementations over the existing repository/PostgreSQL layer.
//
// Scope note, matching this session's established "basics" precedent:
//   - one Orchestrator is built per provisioning run from its already-
//     resolved manifest (held in closures, not rehydrated from
//     TenantProvisioning.Metadata); persisting/rehydrating the resolved
//     manifest across process restarts is a follow-up, not this pass's job.
//   - a single CapabilityScope is used for every grant/binding in a
//     manifest (the manifest format itself has no per-resource scope
//     concept yet).
//   - RECONCILE's "observed" reads are read-after-write verification against
//     this same repository, not an external provider -- there is no external
//     provider integration in this codebase yet for RECONCILE to observe.
package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/resolver"
	"github.com/nabhold/baobab-cp/internal/store"
)

// ContextAuthorityRepo is the subset of internal/repository.Repository /
// internal/repository.PostgresRepository that AuthoritativeContextResolver
// needs beyond Tenant (which store.TenantStore already owns -- Tenant is
// that module's own aggregate, ADR-BCP-010 §41).
type ContextAuthorityRepo interface {
	GetMarket(ctx context.Context, id string) (domain.Market, error)
	ListMarketAssignmentsForTenant(ctx context.Context, tenantID string) ([]domain.MarketAssignment, error)
	GetDigitalEstate(ctx context.Context, id string) (domain.DigitalEstate, error)
	GetIsolationProfile(ctx context.Context, id string) (domain.IsolationProfile, error)
	// GetCanonicalEntity backs the OrganisationID resolution stage
	// (ADR-BCP-016).
	GetCanonicalEntity(ctx context.Context, id string) (domain.CanonicalEntity, error)
	// ListTenantOrganisationMappings backs organisation attestation
	// (ADR-BCP-018 gate ORG-14).
	ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error)
}

// ContextAuthorityAdapter composes store.TenantStore with
// ContextAuthorityRepo to satisfy ContextAuthority, so this module does not
// need a second Tenant lookup on the repository package (see
// ContextResolutionService, which established this same store.TenantStore
// dependency for the tenant/legal-entity stages ZB-02 builds on top of).
type ContextAuthorityAdapter struct {
	Tenants store.TenantStore
	Repo    ContextAuthorityRepo
}

func (a ContextAuthorityAdapter) GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	return a.Tenants.GetTenant(ctx, tenantID)
}
func (a ContextAuthorityAdapter) GetMarket(ctx context.Context, marketID string) (domain.Market, error) {
	return a.Repo.GetMarket(ctx, marketID)
}
func (a ContextAuthorityAdapter) ListMarketAssignmentsForTenant(ctx context.Context, tenantID string) ([]domain.MarketAssignment, error) {
	return a.Repo.ListMarketAssignmentsForTenant(ctx, tenantID)
}
func (a ContextAuthorityAdapter) GetDigitalEstate(ctx context.Context, estateID string) (domain.DigitalEstate, error) {
	return a.Repo.GetDigitalEstate(ctx, estateID)
}
func (a ContextAuthorityAdapter) GetIsolationProfile(ctx context.Context, profileID string) (domain.IsolationProfile, error) {
	return a.Repo.GetIsolationProfile(ctx, profileID)
}
func (a ContextAuthorityAdapter) GetCanonicalEntity(ctx context.Context, id string) (domain.CanonicalEntity, error) {
	return a.Repo.GetCanonicalEntity(ctx, id)
}

func (a ContextAuthorityAdapter) ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error) {
	return a.Repo.ListTenantOrganisationMappings(ctx, tenantID, at)
}

var _ ContextAuthority = ContextAuthorityAdapter{}

// ZB02Repository is every persistence dependency the ZB-02 pipeline needs
// beyond Tenant/TenantProvisioning. *repository.Repository and
// *repository.PostgresRepository both satisfy it already.
type ZB02Repository interface {
	CapabilityGrantStore
	CapabilityBindingStore
	ContextAuthorityRepo
	ManifestRegistry
	CreateCapabilityScope(ctx context.Context, scope capabilitydomain.CapabilityScope) error
	GetEffectiveMarketAssignment(ctx context.Context, tenantID, marketID string, at time.Time) (domain.MarketAssignment, error)
	CreateMarketAssignment(ctx context.Context, assignment domain.MarketAssignment) error
	GetTradeLane(ctx context.Context, tenantID, tradeLaneID string) (domain.TradeLane, error)
	SaveTradeLane(ctx context.Context, lane domain.TradeLane) error
	ReadinessSnapshotStore
	DriftSnapshotStore
}

// ZB02Dependencies is the wiring BuildZB02Pipeline needs. Now defaults to
// time.Now().UTC() when nil.
type ZB02Dependencies struct {
	Tenants      store.TenantStore
	Repo         ZB02Repository
	Provisioning TenantProvisioningStore
	Now          func() time.Time
}

// EnsureDefaultCapabilityScope returns the tenant-wide, unconstrained
// CapabilityScope this manifest's grants/bindings resolve against,
// creating it if it does not already exist. One scope per tenant is this
// pass's deliberate simplification (see this file's doc comment).
func EnsureDefaultCapabilityScope(ctx context.Context, repo interface {
	GetCapabilityScope(ctx context.Context, scopeID string) (capabilitydomain.CapabilityScope, error)
	CreateCapabilityScope(ctx context.Context, scope capabilitydomain.CapabilityScope) error
}, tenantID string) (string, error) {
	// capability.capability_scope.scope_id is a database uuid column, so the
	// id must be a well-formed UUID, not an arbitrary readable string. It is
	// deterministic (derived from tenantID alone) so repeated APPLY runs for
	// the same tenant reuse the same scope instead of minting a new one.
	scopeID := deterministicUUID("zb02-default-scope|" + tenantID)
	if _, err := repo.GetCapabilityScope(ctx, scopeID); err == nil {
		return scopeID, nil
	}
	scope := capabilitydomain.CapabilityScope{ScopeID: scopeID, TenantID: tenantID}
	if err := repo.CreateCapabilityScope(ctx, scope); err != nil {
		return "", fmt.Errorf("create default capability scope: %w", err)
	}
	return scopeID, nil
}

// deterministicUUID formats a sha256 digest of seed as an RFC 4122-shaped
// UUID string (version/variant nibbles fixed so it reads as a valid UUID
// literal). It is not used as a security-relevant identifier -- only as a
// stable, collision-resistant key derived from stable inputs (e.g. tenant
// ID) so idempotent resource lookups do not need a separate natural-key
// index.
func deterministicUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	sum[6] = (sum[6] & 0x0f) | 0x40 // version 4
	sum[8] = (sum[8] & 0x3f) | 0x80 // variant 10
	hexed := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

// BuildZB02Pipeline composes one Orchestrator for manifest, wiring APPLY,
// RECONCILE and READY over deps.Repo following ZB02ApplyOrder.
func BuildZB02Pipeline(deps ZB02Dependencies, manifest ResolvedManifest, scopeID string) (*Orchestrator, error) {
	if deps.Repo == nil || deps.Tenants == nil || deps.Provisioning == nil {
		return nil, errors.New("ZB-02 pipeline dependencies are incomplete")
	}
	if strings.TrimSpace(scopeID) == "" {
		return nil, errors.New("capability scope id is required")
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	grantProvisioner := NewCapabilityGrantProvisioner(deps.Repo)
	bindingProvisioner := NewCapabilityBindingProvisioner(deps.Repo)
	contextResolver := NewAuthoritativeContextResolver(ContextAuthorityAdapter{Tenants: deps.Tenants, Repo: deps.Repo})

	applyWorker := ApplyWorker{Steps: []ApplyStep{
		marketParticipationApplyStep{repo: deps.Repo, manifest: manifest, newID: domain.NewUUIDv7, now: now},
		capabilityGrantApplyStep{provisioner: grantProvisioner, scopeID: scopeID, manifest: manifest, now: now},
		capabilityBindingApplyStep{provisioner: bindingProvisioner, scopeID: scopeID, manifest: manifest, now: now},
		tradeLaneApplyStep{repo: deps.Repo, marketAssignments: deps.Repo, manifest: manifest, now: now},
	}}

	reconcileWorker := ReconcileWorker{Reconciler: DesiredObservedReconciler{Resources: []ResourceReconciler{
		HashResourceReconciler{
			ResourceType: "market-participation",
			DesiredReader: funcDesiredReader(func(context.Context, provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return desiredMarketParticipation(manifest), nil
			}),
			ObservedReader: funcObservedReader(func(ctx context.Context, _ provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return observedMarketParticipation(ctx, deps.Repo, manifest, now()), nil
			}),
		},
		HashResourceReconciler{
			ResourceType: "capability-grant",
			DesiredReader: funcDesiredReader(func(context.Context, provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return desiredCapabilityGrants(manifest, scopeID), nil
			}),
			ObservedReader: funcObservedReader(func(ctx context.Context, _ provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return observedCapabilityGrants(ctx, deps.Repo, manifest, scopeID), nil
			}),
		},
		HashResourceReconciler{
			ResourceType: "capability-binding-engine-instance",
			DesiredReader: funcDesiredReader(func(context.Context, provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return desiredCapabilityBindings(manifest, scopeID), nil
			}),
			ObservedReader: funcObservedReader(func(ctx context.Context, _ provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return observedCapabilityBindings(ctx, deps.Repo, manifest, scopeID), nil
			}),
		},
		HashResourceReconciler{
			ResourceType: "trade-lane",
			DesiredReader: funcDesiredReader(func(context.Context, provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return desiredTradeLanes(manifest), nil
			}),
			ObservedReader: funcObservedReader(func(ctx context.Context, _ provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
				return observedTradeLanes(ctx, deps.Repo, manifest), nil
			}),
		},
	}}, Snapshots: deps.Repo}

	readinessWorker := ReadinessWorker{Evaluator: NewReadinessEvaluator(
		NewProbeCheck("market-participation", marketParticipationProbe{repo: deps.Repo, manifest: manifest}),
		NewProbeCheck("capability-grants", capabilityGrantsProbe{repo: deps.Repo, manifest: manifest, scopeID: scopeID}),
		NewProbeCheck("capability-bindings", capabilityBindingsProbe{repo: deps.Repo, manifest: manifest, scopeID: scopeID}),
		NewProbeCheck("engine-instances", engineInstancesProbe{repo: deps.Repo, manifest: manifest}),
		NewProbeCheck("context-resolution", contextResolutionProbe{resolver: contextResolver, manifest: manifest}),
		NewProbeCheck("trade-lanes", tradeLanesProbe{repo: deps.Repo, manifest: manifest}),
		NewProbeCheck("isolation-and-residency", isolationResidencyProbe{repo: deps.Repo, manifest: manifest}),
	), Snapshots: deps.Repo}

	return NewOrchestrator(deps.Provisioning, applyWorker, reconcileWorker, readinessWorker), nil
}

// -- APPLY --------------------------------------------------------------

type marketAssignmentReader interface {
	GetEffectiveMarketAssignment(ctx context.Context, tenantID, marketID string, at time.Time) (domain.MarketAssignment, error)
}
type marketAssignmentWriter interface {
	CreateMarketAssignment(ctx context.Context, assignment domain.MarketAssignment) error
}

type marketParticipationApplyStep struct {
	repo interface {
		marketAssignmentReader
		marketAssignmentWriter
	}
	manifest ResolvedManifest
	newID    func() string
	now      func() time.Time
}

func (marketParticipationApplyStep) Key() string { return "market-participation" }
func (s marketParticipationApplyStep) Apply(ctx context.Context, _ provisioningdomain.TenantProvisioning) error {
	at := s.now()
	for _, mk := range s.manifest.Markets {
		if existing, err := s.repo.GetEffectiveMarketAssignment(ctx, s.manifest.TenantID, mk.MarketID, at); err == nil && capabilitiesHash(existing.Capabilities) == capabilitiesHash(mk.Capabilities) {
			continue // idempotent: desired state already materialised
		}
		assignment := domain.MarketAssignment{
			ID: s.newID(), TenantID: s.manifest.TenantID, LegalEntityID: s.manifest.LegalEntityID,
			MarketID: mk.MarketID, Capabilities: mk.Capabilities, EffectiveFrom: at,
			Status: domain.MarketParticipationActive, Source: domain.MarketParticipationSourceProvisioning,
			SourceReference: "zb02-manifest", PolicyVersion: "1",
		}
		if err := s.repo.CreateMarketAssignment(ctx, assignment); err != nil {
			return fmt.Errorf("materialise market participation for %s: %w", mk.Code, err)
		}
	}
	return nil
}

type capabilityGrantApplyStep struct {
	provisioner *CapabilityGrantProvisioner
	scopeID     string
	manifest    ResolvedManifest
	now         func() time.Time
}

func (capabilityGrantApplyStep) Key() string { return "capability-grant" }
func (s capabilityGrantApplyStep) Apply(ctx context.Context, _ provisioningdomain.TenantProvisioning) error {
	desired := make([]DesiredCapabilityGrant, 0, len(s.manifest.CapabilityGrants))
	for _, g := range s.manifest.CapabilityGrants {
		desired = append(desired, DesiredCapabilityGrant{
			TenantID: s.manifest.TenantID, CapabilityKey: g.CapabilityKey, ScopeID: s.scopeID,
			Source: g.Source, SourceReference: g.SourceReference, EffectiveFrom: s.now(),
		})
	}
	if len(desired) == 0 {
		return nil
	}
	_, err := s.provisioner.Reconcile(ctx, desired)
	return err
}

type capabilityBindingApplyStep struct {
	provisioner *CapabilityBindingProvisioner
	scopeID     string
	manifest    ResolvedManifest
	now         func() time.Time
}

func (capabilityBindingApplyStep) Key() string { return "capability-binding-engine-instance" }
func (s capabilityBindingApplyStep) Apply(ctx context.Context, _ provisioningdomain.TenantProvisioning) error {
	trusted := resolver.Context{TenantID: s.manifest.TenantID}
	for _, b := range s.manifest.CapabilityBindings {
		desired := DesiredCapabilityBinding{
			CapabilityKey: b.CapabilityKey, EngineID: b.EngineID, EngineInstanceID: b.EngineInstanceID,
			ScopeID: s.scopeID, BindingMode: b.Mode, Priority: b.Priority, ContractVersion: "v1",
			EffectiveFrom: s.now(),
		}
		if _, _, err := s.provisioner.Apply(ctx, desired, trusted); err != nil {
			return fmt.Errorf("materialise capability binding %s: %w", b.CapabilityKey, err)
		}
	}
	return nil
}

type tradeLaneApplyStep struct {
	repo interface {
		GetTradeLane(ctx context.Context, tenantID, tradeLaneID string) (domain.TradeLane, error)
		SaveTradeLane(ctx context.Context, lane domain.TradeLane) error
	}
	marketAssignments marketAssignmentReader
	manifest          ResolvedManifest
	now               func() time.Time
}

func (tradeLaneApplyStep) Key() string { return "trade-lane" }
func (s tradeLaneApplyStep) Apply(ctx context.Context, _ provisioningdomain.TenantProvisioning) error {
	at := s.now()
	for _, l := range s.manifest.TradeLanes {
		id := deterministicTradeLaneID(s.manifest.TenantID, l.OriginMarketID, l.DestinationMarketID, l.Direction)

		origin, err := s.marketAssignments.GetEffectiveMarketAssignment(ctx, s.manifest.TenantID, l.OriginMarketID, at)
		if err != nil {
			return fmt.Errorf("trade lane %s: load origin participation: %w", id, err)
		}
		destination, err := s.marketAssignments.GetEffectiveMarketAssignment(ctx, s.manifest.TenantID, l.DestinationMarketID, at)
		if err != nil {
			return fmt.Errorf("trade lane %s: load destination participation: %w", id, err)
		}

		lane := domain.TradeLane{
			ID: id, TenantID: s.manifest.TenantID, OriginMarketID: l.OriginMarketID, DestinationMarketID: l.DestinationMarketID,
			Direction: l.Direction, Status: domain.TradeLaneSuspended, PermittedCapabilityKeys: l.PermittedCapabilityKeys,
			CreatedAt: at, UpdatedAt: at,
		}
		if err := domain.ValidateTradeLaneParticipation(lane, origin, destination, at); err != nil {
			// Origin/destination participation is not yet effective (this
			// APPLY may be racing an earlier market-participation step that
			// has not converged yet). Persist SUSPENDED and let a later
			// APPLY re-run activate it once participation is effective.
			if saveErr := s.repo.SaveTradeLane(ctx, lane); saveErr != nil {
				return fmt.Errorf("trade lane %s: persist pending lane: %w", id, saveErr)
			}
			continue
		}
		lane.Status = domain.TradeLaneActive
		if err := s.repo.SaveTradeLane(ctx, lane); err != nil {
			return fmt.Errorf("trade lane %s: activate: %w", id, err)
		}
	}
	return nil
}

func deterministicTradeLaneID(tenantID, originMarketID, destinationMarketID string, direction domain.TradeLaneDirection) string {
	sum := sha256.Sum256([]byte(tenantID + "|" + originMarketID + "|" + destinationMarketID + "|" + string(direction)))
	return "tlane_" + hex.EncodeToString(sum[:])[:16]
}

// -- RECONCILE ------------------------------------------------------------

type funcDesiredReader func(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]ResourceObservation, error)

func (f funcDesiredReader) Desired(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
	return f(ctx, op)
}

type funcObservedReader func(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]ResourceObservation, error)

func (f funcObservedReader) Observed(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]ResourceObservation, error) {
	return f(ctx, op)
}

func capabilitiesHash(caps []domain.MarketParticipationCapability) string {
	sorted := append([]domain.MarketParticipationCapability(nil), caps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	parts := make([]string, len(sorted))
	for i, c := range sorted {
		parts[i] = string(c)
	}
	return strings.Join(parts, ",")
}

func desiredMarketParticipation(manifest ResolvedManifest) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.Markets))
	for _, mk := range manifest.Markets {
		out = append(out, ResourceObservation{ResourceType: "market-participation", ResourceKey: mk.MarketID, DesiredHash: capabilitiesHash(mk.Capabilities)})
	}
	return out
}

func observedMarketParticipation(ctx context.Context, repo marketAssignmentReader, manifest ResolvedManifest, at time.Time) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.Markets))
	for _, mk := range manifest.Markets {
		a, err := repo.GetEffectiveMarketAssignment(ctx, manifest.TenantID, mk.MarketID, at)
		if err != nil {
			out = append(out, ResourceObservation{ResourceType: "market-participation", ResourceKey: mk.MarketID, Present: false})
			continue
		}
		out = append(out, ResourceObservation{ResourceType: "market-participation", ResourceKey: mk.MarketID, ObservedHash: capabilitiesHash(a.Capabilities), Present: true})
	}
	return out
}

func grantKey(capabilityKey, scopeID string) string { return capabilityKey + "|" + scopeID }

func desiredCapabilityGrants(manifest ResolvedManifest, scopeID string) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.CapabilityGrants))
	for _, g := range manifest.CapabilityGrants {
		out = append(out, ResourceObservation{ResourceType: "capability-grant", ResourceKey: grantKey(g.CapabilityKey, scopeID), DesiredHash: "present"})
	}
	return out
}

func observedCapabilityGrants(ctx context.Context, repo CapabilityGrantStore, manifest ResolvedManifest, scopeID string) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.CapabilityGrants))
	for _, g := range manifest.CapabilityGrants {
		key := grantKey(g.CapabilityKey, scopeID)
		grants, err := repo.ListGrants(ctx, manifest.TenantID, g.CapabilityKey)
		if err != nil {
			out = append(out, ResourceObservation{ResourceType: "capability-grant", ResourceKey: key, Present: false})
			continue
		}
		present := false
		for _, grant := range grants {
			if grant.ScopeID == scopeID && grant.Status == capabilitydomain.GrantStatusActive {
				present = true
				break
			}
		}
		observed := ResourceObservation{ResourceType: "capability-grant", ResourceKey: key, Present: present}
		if present {
			observed.ObservedHash = "present"
		}
		out = append(out, observed)
	}
	return out
}

func bindingKey(capabilityKey, engineInstanceID string) string {
	return capabilityKey + "|" + engineInstanceID
}

func desiredCapabilityBindings(manifest ResolvedManifest, scopeID string) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.CapabilityBindings))
	for _, b := range manifest.CapabilityBindings {
		out = append(out, ResourceObservation{ResourceType: "capability-binding-engine-instance", ResourceKey: bindingKey(b.CapabilityKey, b.EngineInstanceID), DesiredHash: "present"})
	}
	return out
}

func observedCapabilityBindings(ctx context.Context, repo CapabilityBindingStore, manifest ResolvedManifest, scopeID string) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.CapabilityBindings))
	for _, b := range manifest.CapabilityBindings {
		key := bindingKey(b.CapabilityKey, b.EngineInstanceID)
		bindings, err := repo.ListBindings(ctx, b.CapabilityKey)
		if err != nil {
			out = append(out, ResourceObservation{ResourceType: "capability-binding-engine-instance", ResourceKey: key, Present: false})
			continue
		}
		present := false
		for _, binding := range bindings {
			if binding.EngineInstanceID == b.EngineInstanceID && binding.ScopeID == scopeID && binding.Status == "ACTIVE" {
				present = true
				break
			}
		}
		observed := ResourceObservation{ResourceType: "capability-binding-engine-instance", ResourceKey: key, Present: present}
		if present {
			observed.ObservedHash = "present"
		}
		out = append(out, observed)
	}
	return out
}

func tradeLaneKey(l ResolvedTradeLane) string {
	return deterministicTradeLaneID("", l.OriginMarketID, l.DestinationMarketID, l.Direction)
}

func desiredTradeLanes(manifest ResolvedManifest) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.TradeLanes))
	for _, l := range manifest.TradeLanes {
		out = append(out, ResourceObservation{ResourceType: "trade-lane", ResourceKey: tradeLaneKey(l), DesiredHash: "ACTIVE"})
	}
	return out
}

func observedTradeLanes(ctx context.Context, repo interface {
	GetTradeLane(ctx context.Context, tenantID, tradeLaneID string) (domain.TradeLane, error)
}, manifest ResolvedManifest) []ResourceObservation {
	out := make([]ResourceObservation, 0, len(manifest.TradeLanes))
	for _, l := range manifest.TradeLanes {
		id := deterministicTradeLaneID(manifest.TenantID, l.OriginMarketID, l.DestinationMarketID, l.Direction)
		lane, err := repo.GetTradeLane(ctx, manifest.TenantID, id)
		if err != nil {
			out = append(out, ResourceObservation{ResourceType: "trade-lane", ResourceKey: tradeLaneKey(l), Present: false})
			continue
		}
		observed := ResourceObservation{ResourceType: "trade-lane", ResourceKey: tradeLaneKey(l), Present: true}
		if lane.IsUsable() {
			observed.ObservedHash = "ACTIVE"
		} else {
			observed.ObservedHash = string(lane.Status)
		}
		out = append(out, observed)
	}
	return out
}

// -- READY ------------------------------------------------------------

type marketParticipationProbe struct {
	repo     marketAssignmentReader
	manifest ResolvedManifest
}

func (p marketParticipationProbe) Probe(ctx context.Context, tenantID string, at time.Time) (bool, string, string, error) {
	for _, mk := range p.manifest.Markets {
		a, err := p.repo.GetEffectiveMarketAssignment(ctx, tenantID, mk.MarketID, at)
		if err != nil || !a.IsOperationalAt(at) {
			return false, "", fmt.Sprintf("market %s has no operational participation", mk.Code), nil
		}
		if capabilitiesHash(a.Capabilities) != capabilitiesHash(mk.Capabilities) {
			return false, "", fmt.Sprintf("market %s participation capabilities do not match desired state", mk.Code), nil
		}
	}
	return true, "all markets operational", "", nil
}

type capabilityGrantsProbe struct {
	repo     CapabilityGrantStore
	manifest ResolvedManifest
	scopeID  string
}

func (p capabilityGrantsProbe) Probe(ctx context.Context, tenantID string, at time.Time) (bool, string, string, error) {
	for _, g := range p.manifest.CapabilityGrants {
		grants, err := p.repo.ListGrants(ctx, tenantID, g.CapabilityKey)
		if err != nil {
			return false, "", fmt.Sprintf("list grants for %s: %v", g.CapabilityKey, err), nil
		}
		found := false
		for _, grant := range grants {
			if grant.ScopeID == p.scopeID && grant.Status == capabilitydomain.GrantStatusActive && grant.IsEffective(at) {
				found = true
				break
			}
		}
		if !found {
			return false, "", fmt.Sprintf("no effective ACTIVE grant for capability %s", g.CapabilityKey), nil
		}
	}
	return true, "all requested capability grants effective", "", nil
}

type capabilityBindingsProbe struct {
	repo     CapabilityBindingStore
	manifest ResolvedManifest
	scopeID  string
}

func (p capabilityBindingsProbe) Probe(ctx context.Context, tenantID string, at time.Time) (bool, string, string, error) {
	for _, b := range p.manifest.CapabilityBindings {
		bindings, err := p.repo.ListBindings(ctx, b.CapabilityKey)
		if err != nil {
			return false, "", fmt.Sprintf("list bindings for %s: %v", b.CapabilityKey, err), nil
		}
		found := false
		for _, binding := range bindings {
			if binding.EngineInstanceID == b.EngineInstanceID && binding.ScopeID == p.scopeID && binding.Status == "ACTIVE" {
				found = true
				break
			}
		}
		if !found {
			return false, "", fmt.Sprintf("no ACTIVE binding for capability %s", b.CapabilityKey), nil
		}
	}
	return true, "all requested capability bindings active", "", nil
}

type engineInstancesProbe struct {
	repo     CapabilityBindingStore
	manifest ResolvedManifest
}

func (p engineInstancesProbe) Probe(ctx context.Context, _ string, at time.Time) (bool, string, string, error) {
	for _, b := range p.manifest.CapabilityBindings {
		instances, err := p.repo.ListActiveInstances(ctx, b.EngineID)
		if err != nil {
			return false, "", fmt.Sprintf("list instances for engine %s: %v", b.EngineID, err), nil
		}
		found := false
		for _, instance := range instances {
			if instance.ID == b.EngineInstanceID && instance.Status == "ACTIVE" {
				found = true
				break
			}
		}
		if !found {
			return false, "", fmt.Sprintf("engine instance %s is not ACTIVE", b.EngineInstanceID), nil
		}
	}
	return true, "all selected engine instances active", "", nil
}

type contextResolutionProbe struct {
	resolver *AuthoritativeContextResolver
	manifest ResolvedManifest
}

func (p contextResolutionProbe) Probe(ctx context.Context, tenantID string, _ time.Time) (bool, string, string, error) {
	for _, mk := range p.manifest.Markets {
		_, err := p.resolver.Resolve(ctx, ContextResolutionRequest{
			PrincipalID: "zb02-readiness-probe", TenantID: tenantID, LegalEntityID: p.manifest.LegalEntityID,
			MarketID: mk.MarketID, CorrelationID: "zb02-readiness-" + mk.MarketID,
		})
		if err != nil {
			return false, "", fmt.Sprintf("context resolution failed for market %s: %v", mk.Code, err), nil
		}
	}
	return true, "context resolvable for every requested market", "", nil
}

type tradeLanesProbe struct {
	repo interface {
		GetTradeLane(ctx context.Context, tenantID, tradeLaneID string) (domain.TradeLane, error)
	}
	manifest ResolvedManifest
}

func (p tradeLanesProbe) Probe(ctx context.Context, tenantID string, _ time.Time) (bool, string, string, error) {
	for _, l := range p.manifest.TradeLanes {
		id := deterministicTradeLaneID(tenantID, l.OriginMarketID, l.DestinationMarketID, l.Direction)
		lane, err := p.repo.GetTradeLane(ctx, tenantID, id)
		if err != nil || !lane.IsUsable() {
			return false, "", fmt.Sprintf("trade lane %s is not ACTIVE", id), nil
		}
	}
	return true, "all required trade lanes active", "", nil
}

type isolationResidencyProbe struct {
	repo interface {
		GetIsolationProfile(ctx context.Context, id string) (domain.IsolationProfile, error)
	}
	manifest ResolvedManifest
}

func (p isolationResidencyProbe) Probe(ctx context.Context, _ string, _ time.Time) (bool, string, string, error) {
	if strings.TrimSpace(p.manifest.IsolationRequirement) == "" {
		return true, "no isolation requirement declared", "", nil
	}
	profile, err := p.repo.GetIsolationProfile(ctx, p.manifest.IsolationRequirement)
	if err != nil {
		return false, "", fmt.Sprintf("isolation profile %s could not be resolved: %v", p.manifest.IsolationRequirement, err), nil
	}
	return true, profile.ID, "", nil
}
