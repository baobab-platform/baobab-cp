package repository

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPEO04ALegacySharedRegistryCannotGrantVerifiedLegalPersonInStagingOrProduction(t *testing.T) {
	for _, env := range []string{"", "unknown", "staging", "production", "PRODUCTION"} {
		t.Run(env, func(t *testing.T) {
			repo := &PostgresRepository{Environment: env}
			_, err := repo.ApplyFirstPartyGovernance(context.Background(), FirstPartyGovernance{
				LegalEntityID: "NABHOLD", LegalName: "Declared Holdings",
				EvidenceReference: "shared:registry:declaration",
				At: time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC),
			}, AuditActor{ActorType: "workload", ActorID: "synthetic-workload"})
			if err == nil || !strings.Contains(err.Error(), "cannot verify legal incorporation") {
				t.Fatalf("Shared membership unexpectedly granted incorporation in %q: %v", env, err)
			}
		})
	}
}
