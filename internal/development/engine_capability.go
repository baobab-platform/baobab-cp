package development

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/capability/catalogue"
	capdomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (p nativeCapabilityProfile) compositionMembers() []capdomain.CompositionMember {
	out := []capdomain.CompositionMember{}
	for _, key := range p.capabilityKeys() {
		out = append(out, capdomain.CompositionMember{CapabilityKey: key, Criticality: capdomain.MembershipCriticalityMandatory})
	}
	return out
}
func (p nativeCapabilityProfile) recordProjections(out *ExternalCapabilityFixture, projections []productdomain.EntitlementProjection) error {
	keys := p.capabilityKeys()
	if len(projections) != len(keys) {
		return errors.New("complete normal composition expansion required")
	}
	seen := map[string]string{}
	for _, projection := range projections {
		if !slices.Contains(keys, projection.CapabilityKey) || seen[projection.CapabilityKey] != "" || projection.Status != productdomain.EntitlementProjectionStatusMaterialized || projection.GrantID == "" {
			return errors.New("normal composition did not materialize every capability")
		}
		seen[projection.CapabilityKey] = projection.GrantID
	}
	out.GrantID = seen[p.capability]
	if p.engineProfile != nil {
		out.Grants = seen
	}
	return nil
}

// EngineCapabilityFixtures uses exactly the owner source declaration's normal
// DRAFT registration. Profile data cannot change canonical capability meaning.
func EngineCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource, registration []byte) (ExternalCapabilityFixture, error) {
	var empty ExternalCapabilityFixture
	if cfg.IntegrationProfile == nil || cfg.Validate() != nil {
		return empty, errors.New("validated profile required")
	}
	p := cfg.IntegrationProfile
	var bootstrapRaw []byte
	if err := pool.QueryRow(ctx, `SELECT result FROM development.bootstrap_run WHERE run_id=$1 AND expires_at>$2`, cfg.RunID, cfg.Now).Scan(&bootstrapRaw); err != nil {
		return empty, err
	}
	var run Result
	if json.Unmarshal(bootstrapRaw, &run) != nil {
		return empty, errors.New("owned profile bootstrap required")
	}
	digest := sha256.Sum256(p.raw)
	if run.IntegrationProfileDigest != hex.EncodeToString(digest[:]) {
		return empty, errors.New("bootstrap integration profile changed")
	}
	rec, err := billing.ParseRegistration(registration)
	if err != nil {
		return empty, err
	}
	if rec.Repository != p.SubjectEngine || rec.Provider.Ownership != p.SubjectEngine || rec.Provider.ProviderKey != p.ProviderKey || rec.Provider.Lifecycle != "DRAFT" || rec.Provider.Invocation == nil || len(rec.Support) != len(p.Capabilities) {
		return empty, errors.New("owner DRAFT registration mismatch")
	}
	canonical, err := catalogue.Embedded()
	if err != nil {
		return empty, err
	}
	for _, c := range rec.Capabilities {
		matched := false
		for _, known := range canonical {
			if known.Capability.Key == c.Key {
				k := known.Capability
				matched = c.Name == k.Name && c.Description == k.Description && c.DomainKey == k.DomainKey && c.Lifecycle == k.Lifecycle && c.Maturity == k.Maturity
			}
		}
		if !matched {
			return empty, errors.New("canonical catalogue definition required")
		}
	}
	for _, cap := range p.Capabilities {
		found := false
		for _, support := range rec.Support {
			if support.CapabilityKey == cap.Key && len(support.ContractVersions) == 1 && support.ContractVersions[0] == cap.Version {
				found = true
			}
		}
		if !found {
			return empty, errors.New("declared owner support mismatch")
		}
	}
	profile := nativeCapabilityProfile{key: p.ProfileKey, tenantKey: p.EnterpriseSeed, engine: p.SubjectEngine, providerKey: p.ProviderKey, capability: p.Capabilities[0].Key, product: "dev14-" + p.ProfileKey, diagnostic: "ENGINE", engineProfile: p, registrationData: registration}
	profile.readiness = func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://engine.baobab-dev.test"+p.Adapter.ReadinessPath, nil)
		if err != nil {
			return err
		}
		client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 16385))
		if err != nil || len(body) > 16384 || response.StatusCode != 200 || !json.Valid(body) {
			return errors.New("native engine readiness required")
		}
		return nil
	}
	out, err := observedCapabilityFixtures(ctx, pool, cfg, phase, source, profile)
	if err != nil {
		return out, err
	}
	// Every resource is read independently from the authoritative owner tables.
	if phase != "register" {
		for _, key := range profile.capabilityKeys() {
			var grant, binding, projection bool
			err = pool.QueryRow(ctx, `SELECT
    EXISTS(SELECT 1 FROM capability.capability_grant WHERE grant_id::text=$1 AND tenant_id=$2 AND capability_key=$3 AND status='ACTIVE'),
    EXISTS(SELECT 1 FROM capability.capability_binding WHERE id::text=$4 AND capability_key=$3 AND provider_id::text=$5 AND engine_instance_id::text=$6 AND status='ACTIVE'),
    EXISTS(SELECT 1 FROM product.entitlement_projection WHERE subscription_id::text=$7 AND tenant_id=$2 AND capability_key=$3 AND grant_id::text=$1 AND status='MATERIALIZED')`, out.Grants[key], out.TenantID, key, out.Bindings[key], out.ProviderID, out.InstanceID, out.SubscriptionID).Scan(&grant, &binding, &projection)
			if err != nil {
				return out, err
			}
			out.GrantActive = out.GrantActive && grant
			out.BindingActive = out.BindingActive && binding
			out.ProjectionCurrent = out.ProjectionCurrent && projection
		}
	}
	return out, nil
}
