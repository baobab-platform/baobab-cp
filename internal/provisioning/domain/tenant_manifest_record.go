// Target path: internal/provisioning/domain/tenant_manifest_record.go
package domain

import (
	"encoding/json"
	"time"
)

// TenantManifestRecord is the persisted desired-state snapshot a
// TenantProvisioning run was built from (Gate ZB-03.1, migration 000042).
// It exists so restart recovery does not depend on a process's in-memory
// ResolvedManifest closure -- see internal/provisioning/composition_root.go's
// own doc comment, which names this as a deliberate ZB-02 simplification
// closed by this type. One record per TenantProvisioning run (1:1, not
// versioned across retries): a caller that wants different desired state
// creates a new TenantProvisioning with a new idempotency key, matching the
// existing Plan() semantics.
//
// RawManifest is the manifest exactly as submitted (symbolic references:
// market codes, capability keys). ResolvedManifest is that same manifest
// after every symbolic reference was verified against authoritative
// registries -- this is what a restarted process rehydrates and hands
// straight back to BuildZB02Pipeline, without re-resolving against
// registries that may have changed since (an already-applied manifest must
// not silently re-resolve to different canonical IDs on retry). Both are
// internal/provisioning package types (TenantManifest/ResolvedManifest);
// this package stores them as opaque JSON so internal/repository never
// needs to import internal/provisioning.
type TenantManifestRecord struct {
	ID                   string          `json:"id,omitempty"`
	TenantProvisioningID string          `json:"tenant_provisioning_id"`
	TenantID             string          `json:"tenant_id"`
	SchemaVersion        string          `json:"schema_version"`
	ManifestHash         string          `json:"manifest_hash"`
	DesiredStateVersion  int64           `json:"desired_state_version"`
	Source               string          `json:"source"`
	RawManifest          json.RawMessage `json:"raw_manifest"`
	ResolvedManifest     json.RawMessage `json:"resolved_manifest"`
	CreatedAt            time.Time       `json:"created_at,omitempty"`
}
