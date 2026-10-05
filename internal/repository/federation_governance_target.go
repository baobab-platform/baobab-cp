package repository

import (
	"context"
	"errors"
	"time"
)

var ErrFederationGovernanceTargetNotFound = errors.New("federation governance target not found")

// FederationGovernanceTargetQuery is CP's half of target validation. TrustID,
// SnapshotID and TrustRevision remain IAM-owned and are deliberately absent:
// CP proves only the registered native reference, topology and canonical
// platform scope. IAM's composite target resolver must prove the native trust
// revision/snapshot binding before an approval can use the digest.
type FederationGovernanceTargetQuery struct {
	ReferenceID       string
	Kind              string
	SystemNamespace   string
	EngineCode        string
	NativeEntityType  string
	ProviderID        string
	EngineInstanceID  string
	OrganisationID    string
	DigitalEstateID   string
	Environment       string
}

// FederationGovernanceTargetRegistration is non-approval CP evidence. Digest
// is the immutable sha256 fingerprint registered for the native target; it is
// not by itself an IAM approval receipt.
type FederationGovernanceTargetRegistration struct {
	Digest      string
	ReferenceID string
	Environment string
	TenantID    string
}

type FederationGovernanceTargetReader interface {
	ReadFederationGovernanceTargetRegistration(context.Context, FederationGovernanceTargetQuery, time.Time) (FederationGovernanceTargetRegistration, error)
}
