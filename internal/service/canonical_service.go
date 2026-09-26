package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// CanonicalEntityService owns canonical entity lifecycle transitions.
type CanonicalEntityService struct {
	Repository repository.CanonicalEntityRepository
	Now        func() time.Time
	// Organisations suspends organisation-kind entities atomically with
	// their profile and OrganisationSuspended event (ADR-BCP-018 section
	// 124). Nil falls back to the generic transition, which emits nothing.
	Organisations OrganisationSuspender
}

// OrganisationSuspender suspends an organisation-kind canonical entity.
type OrganisationSuspender interface {
	SuspendOrganisationEntity(ctx context.Context, id string, expectedVersion int64, at time.Time, reason string, actor repository.AuditActor) error
}

// organisationSuspensionReason records suspensions made through the generic
// canonical lifecycle API, which carries no reason of its own.
const organisationSuspensionReason = "suspended through the canonical entity lifecycle"

// SuspendAs is Suspend attributed to actor. An organisation-kind entity is
// suspended together with its profile and OrganisationSuspended event.
func (s CanonicalEntityService) SuspendAs(ctx context.Context, id string, expectedVersion int64, actor repository.AuditActor) (domain.CanonicalEntity, error) {
	entity, err := s.Get(ctx, id)
	if err != nil {
		return domain.CanonicalEntity{}, err
	}
	if s.Organisations == nil || !domain.OrganisationEntityTypes[entity.EntityType] {
		return s.Suspend(ctx, id, expectedVersion)
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	if err := s.Organisations.SuspendOrganisationEntity(ctx, id, expectedVersion, now, organisationSuspensionReason, actor); err != nil {
		return domain.CanonicalEntity{}, err
	}
	entity.Status, entity.Version = "SUSPENDED", expectedVersion+1
	return entity, nil
}

func (s CanonicalEntityService) Create(ctx context.Context, entity domain.CanonicalEntity) (domain.CanonicalEntity, error) {
	if s.Repository == nil {
		return domain.CanonicalEntity{}, errors.New("canonical repository is required")
	}
	if entity.Status == "" {
		entity.Status = "DRAFT"
	}
	if entity.SchemaVersion == 0 {
		entity.SchemaVersion = 1
	}
	if entity.EffectiveFrom.IsZero() {
		if s.Now != nil {
			entity.EffectiveFrom = s.Now().UTC()
		} else {
			entity.EffectiveFrom = time.Now().UTC()
		}
	}
	if err := s.Repository.CreateCanonicalEntity(ctx, entity); err != nil {
		return domain.CanonicalEntity{}, err
	}
	entity.Version = 1
	return entity, nil
}

func (s CanonicalEntityService) Get(ctx context.Context, id string) (domain.CanonicalEntity, error) {
	if s.Repository == nil {
		return domain.CanonicalEntity{}, errors.New("canonical repository is required")
	}
	return s.Repository.GetCanonicalEntity(ctx, id)
}

func (s CanonicalEntityService) Validate(ctx context.Context, id string, expectedVersion int64) (domain.CanonicalEntity, error) {
	return s.transition(ctx, id, expectedVersion, "VALIDATED", "DRAFT")
}

func (s CanonicalEntityService) Activate(ctx context.Context, id string, expectedVersion int64) (domain.CanonicalEntity, error) {
	return s.transition(ctx, id, expectedVersion, "ACTIVE", "VALIDATED")
}

func (s CanonicalEntityService) Suspend(ctx context.Context, id string, expectedVersion int64) (domain.CanonicalEntity, error) {
	return s.transition(ctx, id, expectedVersion, "SUSPENDED", "ACTIVE")
}

func (s CanonicalEntityService) Retire(ctx context.Context, id string, expectedVersion int64) (domain.CanonicalEntity, error) {
	entity, err := s.Get(ctx, id)
	if err != nil {
		return domain.CanonicalEntity{}, err
	}
	if entity.Version != expectedVersion {
		return domain.CanonicalEntity{}, fmt.Errorf("%w: %s is at version %d, not %d", repository.ErrCanonicalEntityVersionConflict, id, entity.Version, expectedVersion)
	}
	if entity.Status != "ACTIVE" && entity.Status != "SUSPENDED" && entity.Status != "DEPRECATED" {
		return domain.CanonicalEntity{}, fmt.Errorf("%w: canonical entity %s cannot transition from %s to RETIRED", repository.ErrCanonicalEntityLifecycleConflict, id, entity.Status)
	}
	return s.transition(ctx, id, expectedVersion, "RETIRED", entity.Status)
}

func (s CanonicalEntityService) transition(ctx context.Context, id string, expectedVersion int64, next, required string) (domain.CanonicalEntity, error) {
	entity, err := s.Get(ctx, id)
	if err != nil {
		return domain.CanonicalEntity{}, err
	}
	// A stale version is reported before the transition check, so a caller
	// holding an old copy is told to reload rather than that the change is
	// prohibited (ADR-BCP-022 sections 128-129).
	if entity.Version != expectedVersion {
		return domain.CanonicalEntity{}, fmt.Errorf("%w: %s is at version %d, not %d", repository.ErrCanonicalEntityVersionConflict, id, entity.Version, expectedVersion)
	}
	if entity.Status != required {
		return domain.CanonicalEntity{}, fmt.Errorf("%w: canonical entity %s cannot transition from %s to %s", repository.ErrCanonicalEntityLifecycleConflict, id, entity.Status, next)
	}
	entity.Status = next
	if err := s.Repository.SaveCanonicalEntity(ctx, entity, expectedVersion); err != nil {
		return domain.CanonicalEntity{}, err
	}
	entity.Version = expectedVersion + 1
	return entity, nil
}

func ParseExpectedVersion(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, errors.New("If-Match is required")
	}
	var version int64
	if _, err := fmt.Sscan(strings.Trim(value, "\""), &version); err != nil || version < 1 {
		return 0, errors.New("If-Match must contain a positive entity version")
	}
	return version, nil
}
