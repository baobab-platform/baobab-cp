package organisation

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/capability/certification"
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// PEO05ProviderAuthority reads real CP grants, scoped bindings, immutable
// release identity, current certification, and observed runtime health.
type PEO05ProviderAuthority interface {
	ListGrants(context.Context, string, string) ([]capabilitydomain.CapabilityGrant, error)
	GetCapabilityScope(context.Context, string) (capabilitydomain.CapabilityScope, error)
	ReadPEO05BoundProvider(context.Context, string, string, string, string, time.Time) (repository.PEO05BoundProvider, error)
	GetObservedRelease(context.Context, string, time.Time) (release.ObservedRelease, error)
	GetEngineRelease(context.Context, string) (release.Release, error)
	ListProviderCapabilityCertifications(context.Context, repository.ProviderCapabilityCertificationFilter) ([]certification.Certification, string, error)
	HealthLevels(context.Context, string, string, string) (health.Levels, error)
}

var _ PEO05ProviderAuthority = (*repository.PostgresRepository)(nil)

// PEO05CertifiedProviders never infers runtime support from a provider name
// or a deployment flag. Missing grants, out-of-scope bindings, stale health,
// revoked certifications and release drift all produce a denial.
type PEO05CertifiedProviders struct {
	Store PEO05ProviderAuthority
}

func (p PEO05CertifiedProviders) CurrentMandatoryProviders(ctx context.Context, tenantID, organisationID string,
	plan convergence.Plan, required []string, at time.Time) ([]PEO05ProviderProof, error) {
	if ctx == nil || ctx.Err() != nil || p.Store == nil || tenantID == "" || organisationID == "" ||
		plan.TenantID != tenantID || plan.Expired(at) || len(required) == 0 {
		return nil, ErrPEO05Integrity
	}
	out := make([]PEO05ProviderProof, 0, len(required))
	seen := map[string]bool{}
	for _, key := range required {
		if key == "" || seen[key] {
			return nil, ErrPEO05Integrity
		}
		seen[key] = true
		var binding *convergence.Step
		var grantCount int
		for i := range plan.Steps {
			step := &plan.Steps[i]
			if step.Resources.CapabilityKey != key {
				continue
			}
			switch step.Operation {
			case convergence.OpCreateCapabilityBinding:
				if binding != nil || step.Resources.ProviderKey == "" || step.Resources.EngineInstanceID == "" ||
					step.Resources.BindingMode != "PRIMARY" {
					return nil, ErrPEO05Integrity
				}
				binding = step
			case convergence.OpCreateCapabilityGrant:
				if step.Resources.TenantID != tenantID {
					return nil, ErrPEO05Integrity
				}
				grantCount++
			}
		}
		if binding == nil || grantCount != 1 {
			return nil, ErrPEO05Integrity
		}
		grants, err := p.Store.ListGrants(ctx, tenantID, key)
		if err != nil {
			return nil, ErrPEO05Integrity
		}
		authorised := false
		for _, g := range grants {
			if g.TenantID != tenantID || g.CapabilityKey != key || !g.IsEffective(at) || g.ScopeID == "" {
				continue
			}
			scope, err := p.Store.GetCapabilityScope(ctx, g.ScopeID)
			if err != nil {
				return nil, ErrPEO05Integrity
			}
			if scope.TenantID == tenantID {
				authorised = true
			}
		}
		if !authorised {
			return nil, ErrPEO05Integrity
		}
		b, err := p.Store.ReadPEO05BoundProvider(ctx, tenantID, key,
			binding.Resources.ProviderKey, binding.Resources.EngineInstanceID, at)
		if err != nil || b.CanonicalProviderID == "" || b.ContractMajor < 1 || b.ProviderUUID == "" ||
			b.EngineInstanceUUID == "" || b.ProviderKey != binding.Resources.ProviderKey ||
			b.EngineInstanceKey != binding.Resources.EngineInstanceID || b.Region == "" {
			return nil, ErrPEO05Integrity
		}
		observed, err := p.Store.GetObservedRelease(ctx, b.EngineInstanceKey, at)
		if err != nil || observed.State != release.StateRelease || observed.EngineInstanceID != b.EngineInstanceKey ||
			observed.ReleaseID == "" || observed.ObservationID == "" {
			return nil, ErrPEO05Integrity
		}
		rel, err := p.Store.GetEngineRelease(ctx, observed.ReleaseID)
		if err != nil || rel.Status != release.StatusApproved || rel.EngineID != b.EngineID || rel.ReleaseID != observed.ReleaseID {
			return nil, ErrPEO05Integrity
		}
		supported := false
		for _, s := range rel.ProviderSupport {
			if s.ProviderKey == b.ProviderKey && s.CapabilityKey == key && slices.Contains(s.ContractVersions, b.ContractMajor) {
				supported = true
			}
		}
		if !supported {
			return nil, ErrPEO05Integrity
		}
		cert, err := p.currentCertification(ctx, b, key, rel.ReleaseID, at)
		if err != nil {
			return nil, ErrPEO05Integrity
		}
		levels, err := p.Store.HealthLevels(ctx, b.EngineInstanceUUID, b.ProviderUUID, key)
		if err != nil || !healthyPEO05(levels.EngineInstance, at) ||
			!healthyPEO05(levels.Provider, at) || !healthyPEO05(levels.ProviderCapability, at) {
			return nil, ErrPEO05Integrity
		}
		expiry := at.Add(time.Minute)
		for _, limit := range []time.Time{levels.EngineInstance.ExpiresAt, levels.Provider.ExpiresAt, levels.ProviderCapability.ExpiresAt} {
			if limit.Before(expiry) {
				expiry = limit
			}
		}
		certExpiry := expiry
		if cert.ValidUntil != nil && cert.ValidUntil.Before(certExpiry) {
			certExpiry = *cert.ValidUntil
		}
		if !expiry.After(at) || !certExpiry.After(at) {
			return nil, ErrPEO05Integrity
		}
		observedAt := levels.EngineInstance.ObservedAt
		for _, observation := range []*health.Observation{levels.Provider, levels.ProviderCapability} {
			if observation.ObservedAt.Before(observedAt) {
				observedAt = observation.ObservedAt
			}
		}
		out = append(out, PEO05ProviderProof{
			CapabilityKey: key, TenantID: tenantID, EngineInstanceID: b.EngineInstanceKey,
			ProviderKey: b.ProviderKey, ResidencyRegion: b.Region, SupportStatus: "CERTIFIED",
			CertificationReference: cert.CertificationID, CertificationExpiresAt: certExpiry,
			HealthStatus: "HEALTHY", HealthObservedAt: observedAt, HealthExpiresAt: expiry,
		})
	}
	return out, nil
}

func healthyPEO05(o *health.Observation, at time.Time) bool {
	return o != nil && o.Status == health.StatusHealthy &&
		currentPEO05Observation(o.ObservedAt, o.ExpiresAt, at)
}

func (p PEO05CertifiedProviders) currentCertification(ctx context.Context, b repository.PEO05BoundProvider, key, releaseID string, at time.Time) (certification.Certification, error) {
	filter := repository.ProviderCapabilityCertificationFilter{
		ProviderID: b.CanonicalProviderID, CapabilityKey: key,
		ReleaseID: releaseID, Status: certification.StatusCertified, Limit: 200,
	}
	var found *certification.Certification
	for page := 0; page < 100; page++ {
		items, next, err := p.Store.ListProviderCapabilityCertifications(ctx, filter)
		if err != nil {
			return certification.Certification{}, err
		}
		for _, item := range items {
			if item.ProviderID != b.CanonicalProviderID || item.ProviderKey != b.ProviderKey ||
				item.ContractVersion != b.ContractMajor || item.ReleaseID != releaseID ||
				item.Status != certification.StatusCertified {
				continue
			}
			if item.ValidUntil != nil && !item.ValidUntil.After(at) {
				continue
			}
			if found != nil {
				return certification.Certification{}, ErrPEO05Integrity
			}
			copy := item
			found = &copy
		}
		if next == "" {
			if found != nil {
				return *found, nil
			}
			return certification.Certification{}, errors.New("no current release-bound certification")
		}
		filter.PageToken = next
	}
	return certification.Certification{}, ErrPEO05Integrity
}
