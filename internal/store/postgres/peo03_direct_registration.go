// PEO-03C: a v2-approved onboarding request registers a tenant without
// manufacturing a v1 admission/decision/onboarding row. All authority
// rechecks, PRIMARY binding, tenant, operation, outbox, and FULFILLED status
// commit atomically using Store.registerTenant's transaction.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

// RegisterProgressiveTenantV2 is deliberately separate from legacy
// RegisterTenantV2: the caller cannot switch admission authority by
// including an arbitrary request ID in the older API.
func (s *Store) RegisterProgressiveTenantV2(ctx context.Context, key string,
	metadata basestore.RequestMetadata, c domain.RegisterTenantV2) (domain.Operation, error) {
	if err := c.Validate(); err != nil {
		return domain.Operation{}, err
	}
	if metadata.ActorType != "human" || !domain.IsUUID(metadata.ActorID) {
		return domain.Operation{}, ErrProgressiveBridgeDenied
	}
	step := func(ctx context.Context, tx pgx.Tx, tenantID string) error {
		id, err := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, c.TenantOnboardingRequestID)
		if err != nil {
			return ErrProgressiveBridgeDenied
		}
		tag, err := tx.Exec(ctx, `UPDATE admission.progressive_onboarding_request
			SET status='FULFILLED', tenant_id=$2, fulfilled_at=clock_timestamp()
			WHERE request_id=$1::uuid AND status='AUTHORISED'
			  AND organisation_id=$3::uuid`, id, tenantID, c.OrganisationID)
		if err != nil {
			return fmt.Errorf("fulfil v2 onboarding in registration transaction: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrProgressiveBridgeDenied
		}
		return nil
	}
	return s.registerTenant(ctx, key, metadata, domain.RegisterTenant{
		TenantOnboardingRequestID: c.TenantOnboardingRequestID,
		Basis: domain.RegistrationOnboarding, TenantID: c.TenantID, LegalEntityID: c.LegalEntityID,
		DisplayName: c.DisplayName, IsolationStrategy: c.IsolationStrategy,
		ResidencyRegion: c.ResidencyRegion, RequestedProducts: c.RequestedProducts,
		Metadata: c.Metadata,
	}, &c, step, true)
}

func exactProductSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// verifyProgressiveRegistrationTx is invoked under the SAME SQL transaction
// as registration. The request/Organisation row locks prevent concurrent
// registrations from consuming one authority twice. The applicant, reviewer,
// decider, requester, authoriser and registrar are distinct roles.
func (s *Store) verifyProgressiveRegistrationTx(ctx context.Context, tx pgx.Tx,
	c domain.RegisterTenant, v2 domain.RegisterTenantV2, meta basestore.RequestMetadata) (string, string, error) {
	request, err := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, c.TenantOnboardingRequestID)
	if err != nil {
		return "", "", ErrProgressiveBridgeDenied
	}
	var organisation, status, applicant, reviewer, decider, requester, authoriser, subscription string
	var desiredJSON []byte
	err = tx.QueryRow(ctx, `SELECT o.organisation_id::text,o.status,
		a.applicant_principal_id::text,rv.reviewed_by::text,d.decided_by::text,
		o.requested_by::text,o.authorised_by::text,rv.approved_subscription_type,
		o.desired_state
		FROM admission.progressive_onboarding_request o
		JOIN admission.progressive_admission_decision d ON d.decision_id=o.decision_id
		JOIN admission.progressive_admission_review rv ON rv.review_id=d.review_id
		JOIN admission.client_application_v2 a ON a.client_application_id=o.client_application_id
		JOIN registry.organisation_profile op ON op.canonical_entity_id=o.organisation_id
		JOIN registry.canonical_entity ce ON ce.canonical_entity_id=op.canonical_entity_id
		WHERE o.request_id=$1::uuid AND d.decision='APPROVED'
		  AND a.status='SUBMITTED' AND op.status='ACTIVE'
		  AND ce.entity_type='ORGANISATION' AND ce.tenant_id IS NULL
		FOR UPDATE OF o,ce`, request).
		Scan(&organisation, &status, &applicant, &reviewer, &decider,
			&requester, &authoriser, &subscription, &desiredJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrProgressiveBridgeDenied
	}
	if err != nil {
		return "", "", fmt.Errorf("read progressive registration authority: %w", err)
	}
	if status != "AUTHORISED" || organisation != v2.OrganisationID ||
		meta.ActorID == applicant || meta.ActorID == reviewer || meta.ActorID == decider ||
		meta.ActorID == requester || meta.ActorID == authoriser {
		return "", "", ErrProgressiveBridgeDenied
	}
	var desired domain.OnboardingDesiredState
	if err = json.Unmarshal(desiredJSON, &desired); err != nil {
		return "", "", fmt.Errorf("decode approved v2 desired state: %w", err)
	}
	if desired.DisplayName != c.DisplayName ||
		desired.ResidencyRegion != c.ResidencyRegion ||
		desired.IsolationStrategy != c.IsolationStrategy ||
		!exactProductSet(desired.ProductRequirements, c.RequestedProducts) {
		return "", "", ErrProgressiveBridgeDenied
	}
	// INTERNAL classifications need current actual sponsorship at the
	// moment registration consumes authority. Neither the originating
	// decision nor an outbox event is a durable sponsorship grant.
	if subscription == "INTERNAL" {
		ok, err := bridgeInternalEligible(ctx, tx, organisation)
		if err != nil {
			return "", "", err
		}
		if !ok {
			return "", "", ErrProgressiveBridgeDenied
		}
	}
	return organisation, status, nil
}
