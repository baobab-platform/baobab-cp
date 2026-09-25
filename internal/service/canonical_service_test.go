package service

import (
	"context"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func TestCanonicalEntityLifecycleUsesOptimisticConcurrency(t *testing.T) {
	repo := repository.NewCanonicalRepository()
	service := CanonicalEntityService{Repository: repo, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }}
	entity, err := service.Create(context.Background(), domain.CanonicalEntity{
		ID: "entity-1", CanonicalKey: "tenant:product", EntityType: "PRODUCT", DisplayName: "Product",
		Authority: "baobab", Classification: "INTERNAL",
	})
	if err != nil || entity.Status != "DRAFT" || entity.Version != 1 {
		t.Fatalf("create failed: %+v, %v", entity, err)
	}
	for _, transition := range []struct {
		name string
		call func(int64) (domain.CanonicalEntity, error)
		want string
	}{
		{"validate", func(version int64) (domain.CanonicalEntity, error) {
			return service.Validate(context.Background(), "entity-1", version)
		}, "VALIDATED"},
		{"activate", func(version int64) (domain.CanonicalEntity, error) {
			return service.Activate(context.Background(), "entity-1", version)
		}, "ACTIVE"},
		{"suspend", func(version int64) (domain.CanonicalEntity, error) {
			return service.Suspend(context.Background(), "entity-1", version)
		}, "SUSPENDED"},
		{"retire", func(version int64) (domain.CanonicalEntity, error) {
			return service.Retire(context.Background(), "entity-1", version)
		}, "RETIRED"},
	} {
		entity, err = transition.call(entity.Version)
		if err != nil || entity.Status != transition.want {
			t.Fatalf("%s failed: %+v, %v", transition.name, entity, err)
		}
	}
	if _, err := service.Retire(context.Background(), "entity-1", entity.Version); err == nil {
		t.Fatal("expected invalid retired transition")
	}
	if _, err := service.Validate(context.Background(), "entity-1", 1); err == nil {
		t.Fatal("expected stale version or invalid transition error")
	}
}
