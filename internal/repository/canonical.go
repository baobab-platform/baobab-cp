package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// CanonicalEntityRepository persists canonical registry entities.
type CanonicalEntityRepository interface {
	CreateCanonicalEntity(ctx context.Context, entity domain.CanonicalEntity) error
	GetCanonicalEntity(ctx context.Context, id string) (domain.CanonicalEntity, error)
	SaveCanonicalEntity(ctx context.Context, entity domain.CanonicalEntity, expectedVersion int64) error
}

// CanonicalRepository is an in-memory implementation used by services and tests.
type CanonicalRepository struct {
	mu       sync.RWMutex
	Entities map[string]domain.CanonicalEntity
}

var _ CanonicalEntityRepository = (*CanonicalRepository)(nil)

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
	entity.CreatedAt = time.Now().UTC()
	entity.UpdatedAt = entity.CreatedAt
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
		return domain.CanonicalEntity{}, fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, id)
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
		return fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, entity.ID)
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: %s is at version %d, not %d", ErrCanonicalEntityVersionConflict, entity.ID, current.Version, expectedVersion)
	}
	entity.Version = current.Version + 1
	entity.CreatedAt = current.CreatedAt
	entity.UpdatedAt = time.Now().UTC()
	r.Entities[entity.ID] = entity
	return nil
}
