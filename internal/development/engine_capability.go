package development

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/capability/catalogue"
	capdomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
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
	if err := CheckDatabase(ctx, pool); err != nil {
		return empty, err
	}
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
	if rec.Repository != p.SubjectEngine || rec.Provider.Ownership != p.SubjectEngine || rec.Provider.ProviderKey != p.ProviderKey || rec.Provider.Lifecycle != "DRAFT" || rec.Provider.Invocation == nil {
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
	if phase == "refresh" {
		// A fresh active probe extends health only inside the original run
		// lease; this is never provider activation or certification.
		existing, e := observedCapabilityFixtures(ctx, pool, cfg, "read", source, profile)
		if e != nil {
			return empty, e
		}
		if e = profile.readiness(ctx); e != nil {
			return empty, e
		}
		repo, e := repository.Open(ctx, pool.Config().ConnString())
		if e != nil {
			return empty, e
		}
		defer repo.Close()
		now := time.Now().UTC()
		until := now.Add(2 * time.Minute)
		if run.ExpiresAt.Before(until) {
			until = run.ExpiresAt
		}
		if e = repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: existing.InstanceID}, Status: health.StatusHealthy, ObservedAt: now, ExpiresAt: until, Source: health.SourceActiveProbe, Reasons: []string{}}); e != nil {
			return empty, e
		}
		phase = "read"
	}
	if phase == "rebind" || phase == "suspend-provider" || phase == "disable-principal" {
		existing, e := observedCapabilityFixtures(ctx, pool, cfg, "read", source, profile)
		if e != nil {
			return empty, e
		}
		repo, e := repository.Open(ctx, pool.Config().ConnString())
		if e != nil {
			return empty, e
		}
		defer repo.Close()
		if phase == "rebind" {
			if existing.Phase != "RETIRED" {
				return empty, errors.New("retired owned binding required")
			}
			for _, cap := range p.Capabilities {
				var scope, engine string
				if e = pool.QueryRow(ctx, `SELECT b.scope_id::text,i.engine_id::text FROM capability.capability_binding b JOIN topology.engine_instance i USING(engine_instance_id) WHERE b.id::text=$1 AND b.status='RETIRED' AND i.environment='development'`, existing.Bindings[cap.Key]).Scan(&scope, &engine); e != nil {
					return empty, e
				}
				if e = repo.CreateBinding(ctx, resolver.CapabilityBinding{EngineID: engine, CapabilityKey: cap.Key, EngineInstanceID: existing.InstanceID, ProviderID: existing.ProviderID, ScopeID: scope, BindingMode: capdomain.BindingModePrimary, Priority: 100, Status: "ACTIVE", ContractVersion: fmt.Sprint(cap.Version)}); e != nil {
					return empty, e
				}
				var id string
				if e = pool.QueryRow(ctx, `SELECT b.id::text FROM capability.capability_binding b JOIN capability.capability c USING(capability_id) WHERE b.scope_id::text=$1 AND b.engine_instance_id::text=$2 AND b.provider_id::text=$3 AND b.status='ACTIVE' AND c.code=$4`, scope, existing.InstanceID, existing.ProviderID, cap.Key).Scan(&id); e != nil {
					return empty, e
				}
				existing.Bindings[cap.Key] = id
				if cap.Key == profile.capability {
					existing.BindingID = id
				}
			}
			existing.Phase = "ACTIVE"
		} else {
			// Negative fixture withdrawal only, in the marked disposable
			// store. There is deliberately no status-restoration bypass.
			// Provider activation always used the normal two-human change.
			tx, e := pool.Begin(ctx)
			if e != nil {
				return empty, e
			}
			defer tx.Rollback(ctx)
			target := existing.ProviderID
			var affected int64
			if phase == "suspend-provider" {
				tag, e := tx.Exec(ctx, `UPDATE capability.capability_provider SET status='SUSPENDED',version=version+1 WHERE provider_id::text=$1 AND provider_key=$2 AND status='ACTIVE'`, target, p.ProviderKey)
				if e != nil {
					return empty, e
				}
				affected = tag.RowsAffected()
				existing.Phase = "PROVIDER_SUSPENDED"
			} else {
				target = run.ProofPrincipals[p.IAM["caller"].ClientID]
				tag, e := tx.Exec(ctx, `UPDATE identity.principal p SET status='DISABLED' WHERE p.principal_id=$1 AND p.actor_type='workload' AND p.status='ACTIVE' AND EXISTS(SELECT 1 FROM development.principal_assignment a JOIN identity.external_identity e ON e.principal_id=a.principal_id WHERE a.principal_id=p.principal_id AND a.run_id=$2 AND a.client_id=$3 AND a.tenant_id=$4 AND e.issuer=$5 AND e.subject=$3 AND e.status='ACTIVE')`, target, cfg.RunID, p.IAM["caller"].ClientID, existing.TenantID, cfg.WorkloadIssuerValue())
				if e != nil {
					return empty, e
				}
				affected = tag.RowsAffected()
				existing.Phase = "PRINCIPAL_DISABLED"
			}
			if affected != 1 {
				return empty, errors.New("owned active fixture target required")
			}
			audit, _ := json.Marshal(map[string]any{"run_id": cfg.RunID, "profile_digest": run.IntegrationProfileDigest, "target_id": target, "source_sha": source.SourceSHA, "synthetic_only": true, "protected_acceptance": false})
			if _, e = tx.Exec(ctx, `INSERT INTO audit_events(action,payload) VALUES($1,$2)`, "development.engine-fixture."+phase, audit); e != nil {
				return empty, e
			}
			if e = tx.Commit(ctx); e != nil {
				return empty, e
			}
		}
		raw, e := json.Marshal(existing)
		if e != nil {
			return empty, e
		}
		if _, e = pool.Exec(ctx, `UPDATE development.native_capability_fixture SET result=$3 WHERE run_id=$1 AND profile=$2`, cfg.RunID, profile.key, raw); e != nil {
			return empty, e
		}
		phase = "read"
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
    EXISTS(SELECT 1 FROM capability.capability_grant g JOIN capability.capability c USING(capability_id) WHERE g.grant_id::text=$1 AND g.tenant_id=$2 AND c.code=$3 AND g.status='ACTIVE'),
    EXISTS(SELECT 1 FROM capability.capability_binding b JOIN capability.capability c USING(capability_id) WHERE b.id::text=$4 AND c.code=$3 AND b.provider_id::text=$5 AND b.engine_instance_id::text=$6 AND b.status='ACTIVE'),
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
