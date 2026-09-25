// Target path: internal/provisioning/manifest_persistence.go
//
// Gate ZB-03.1: closes the deferred ZB-02 item "manifest rehydration across
// process restarts" (docs/reconciliation/gate-zb02-completion-report.md §2;
// composition_root.go's own doc comment). BuildZB02Pipeline needs a
// ResolvedManifest; before this file, the only way to get one was to
// re-resolve a TenantManifest in the calling process's memory. These two
// functions are the write/read halves of persisting that resolved manifest
// (internal/repository.TenantManifestRepository, migration 000042) so a
// process that restarts mid-provisioning can rebuild the exact same
// pipeline instead of re-resolving against registries that may have
// changed since the original APPLY.
package provisioning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
)

// NewTenantManifestRecord builds the persisted snapshot of m/resolved for
// provisioningID. source identifies who submitted it (e.g. "http-api",
// "test") for audit purposes only -- it has no behavioural effect.
func NewTenantManifestRecord(provisioningID string, m TenantManifest, resolved ResolvedManifest, source string) (provisioningdomain.TenantManifestRecord, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return provisioningdomain.TenantManifestRecord{}, fmt.Errorf("marshal manifest: %w", err)
	}
	resolvedJSON, err := json.Marshal(resolved)
	if err != nil {
		return provisioningdomain.TenantManifestRecord{}, fmt.Errorf("marshal resolved manifest: %w", err)
	}
	return provisioningdomain.TenantManifestRecord{
		TenantProvisioningID: provisioningID,
		TenantID:             resolved.TenantID,
		SchemaVersion:        m.APIVersion,
		ManifestHash:         manifestHash(raw),
		DesiredStateVersion:  resolved.DesiredStateVersion,
		Source:               source,
		RawManifest:          raw,
		ResolvedManifest:     resolvedJSON,
	}, nil
}

// RehydrateResolvedManifest reverses the resolved side of
// NewTenantManifestRecord, returning exactly the ResolvedManifest
// BuildZB02Pipeline needs to resume an in-flight (or retried) provisioning
// operation after a process restart.
func RehydrateResolvedManifest(record provisioningdomain.TenantManifestRecord) (ResolvedManifest, error) {
	var resolved ResolvedManifest
	if err := json.Unmarshal(record.ResolvedManifest, &resolved); err != nil {
		return ResolvedManifest{}, fmt.Errorf("unmarshal resolved manifest: %w", err)
	}
	return resolved, nil
}

func manifestHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
