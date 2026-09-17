package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/nabhold/baobab-cp/internal/domain"
)

// CanonicalEntityRepository persists canonical registry entities.
type CanonicalEntityRepository interface {
	CreateCanonicalEntity(ctx context.Context, entity domain.CanonicalEntity) error
	GetCanonicalEntity(ctx context.Context, id string) (domain.CanonicalEntity, error)
	SaveCanonicalEntity(ctx context.Context, entity domain.CanonicalEntity, expectedVersion int64) error
}

// ExternalReferenceRepository links a CanonicalEntity to an identifier in
// another system (registry.external_reference, migration 000010), e.g. a
// Keycloak Organization ID (ADR-BCP-016, ADR-BCP-014 §18-19: "Canonical
// Organisation -> ExternalReference -> Keycloak Organization" is the only
// permitted path from identity-side affiliation to canonical authority --
// no engine may treat a Keycloak Organization claim as canonical directly).
type ExternalReferenceRepository interface {
	// CreateExternalReference links entity.CanonicalEntityID to the native
	// system identified by entity.EngineID/NativeType/NativeID. Fails if
	// that exact (canonical_entity_id, provider, provider_key) triple is
	// already linked (registry.external_reference's UNIQUE constraint).
	CreateExternalReference(ctx context.Context, ref domain.ExternalReference) (domain.ExternalReference, error)
	// GetCanonicalEntityByExternalReference resolves a native-system
	// identifier (e.g. a caller-asserted Keycloak Organization ID) back to
	// the CanonicalEntity it was linked to under CreateExternalReference --
	// the reverse direction of the lookup, used by onboarding/backfill
	// flows, never by a runtime authorization decision (which must always
	// verify a caller-asserted organisation_id names a CanonicalEntity
	// directly, per ADR-BCP-016 -- this method is how that value gets
	// populated in the first place, not how it is verified per-request).
	GetCanonicalEntityByExternalReference(ctx context.Context, engineID, nativeType, nativeID string) (domain.CanonicalEntity, error)
}

// CanonicalRepository is an in-memory implementation used by services and tests.
type CanonicalRepository struct {
	mu                 sync.RWMutex
	Entities           map[string]domain.CanonicalEntity
	ExternalReferences []domain.ExternalReference
}

var (
	_ CanonicalEntityRepository   = (*CanonicalRepository)(nil)
	_ ExternalReferenceRepository = (*CanonicalRepository)(nil)
)

func NewCanonicalRepository() *CanonicalRepository {
	return &CanonicalRepository{Entities: map[string]domain.CanonicalEntity{}}
}

func (r *CanonicalRepository) CreateCanonicalEntity(_ context.Context, entity domain.CanonicalEntity) error {
	if r == nil {
		return errors.New("canonical repository is nil")
	}
	if err := entity.Validate(); err != nil {
		return fmt.Errorf("validate canonical entity: %w", err)
	}
	if entity.ID == "" {
		return errors.New("canonical entity id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.Entities[entity.ID]; exists {
		return fmt.Errorf("canonical entity %s already exists", entity.ID)
	}
	entity.Version = 1
	r.Entities[entity.ID] = entity
	return nil
}

func (r *CanonicalRepository) GetCanonicalEntity(_ context.Context, id string) (domain.CanonicalEntity, error) {
	if r == nil {
		return domain.CanonicalEntity{}, errors.New("canonical repository is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entity, exists := r.Entities[id]
	if !exists {
		return domain.CanonicalEntity{}, fmt.Errorf("canonical entity %s not found", id)
	}
	return entity, nil
}

func (r *CanonicalRepository) SaveCanonicalEntity(_ context.Context, entity domain.CanonicalEntity, expectedVersion int64) error {
	if r == nil {
		return errors.New("canonical repository is nil")
	}
	if err := entity.Validate(); err != nil {
		return fmt.Errorf("validate canonical entity: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.Entities[entity.ID]
	if !exists {
		return fmt.Errorf("canonical entity %s not found", entity.ID)
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("canonical entity %s version conflict: expected %d, got %d", entity.ID, expectedVersion, current.Version)
	}
	entity.Version = current.Version + 1
	r.Entities[entity.ID] = entity
	return nil
}

func (r *CanonicalRepository) CreateExternalReference(_ context.Context, ref domain.ExternalReference) (domain.ExternalReference, error) {
	if r == nil {
		return domain.ExternalReference{}, errors.New("canonical repository is nil")
	}
	if err := ref.Validate(); err != nil {
		return domain.ExternalReference{}, fmt.Errorf("validate external reference: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.Entities[ref.CanonicalEntityID]; !exists {
		return domain.ExternalReference{}, fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, ref.CanonicalEntityID)
	}
	for _, existing := range r.ExternalReferences {
		if existing.CanonicalEntityID == ref.CanonicalEntityID && existing.EngineID == ref.EngineID &&
			existing.NativeType == ref.NativeType && existing.NativeID == ref.NativeID {
			return domain.ExternalReference{}, ErrExternalReferenceAlreadyLinked
		}
	}
	if ref.ID == "" {
		ref.ID = domain.NewUUIDv7()
	}
	r.ExternalReferences = append(r.ExternalReferences, ref)
	return ref, nil
}

func (r *CanonicalRepository) GetCanonicalEntityByExternalReference(_ context.Context, engineID, nativeType, nativeID string) (domain.CanonicalEntity, error) {
	if r == nil {
		return domain.CanonicalEntity{}, errors.New("canonical repository is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ref := range r.ExternalReferences {
		if ref.EngineID == engineID && ref.NativeType == nativeType && ref.NativeID == nativeID {
			entity, exists := r.Entities[ref.CanonicalEntityID]
			if !exists {
				return domain.CanonicalEntity{}, fmt.Errorf("canonical entity %s not found", ref.CanonicalEntityID)
			}
			return entity, nil
		}
	}
	return domain.CanonicalEntity{}, fmt.Errorf("external reference %s/%s/%s not found", engineID, nativeType, nativeID)
}
