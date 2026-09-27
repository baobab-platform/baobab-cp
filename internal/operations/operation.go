// Package operations models durable long-running operations (ADR-BCP-022
// sections 54-67, ADR-SHARED-015): work accepted with 202 and polled until it
// finishes. An operation's status is the operation's own, never its
// subject's: SUCCEEDED does not mean a tenant is ACTIVE.
package operations

import (
	"encoding/json"
	"regexp"
	"time"
)

// Status is an operation's lifecycle status (Shared execution-operation
// operationStatus).
type Status string

const (
	StatusQueued             Status = "QUEUED"
	StatusPreparing          Status = "PREPARING"
	StatusRunning            Status = "RUNNING"
	StatusWaiting            Status = "WAITING"
	StatusVerifying          Status = "VERIFYING"
	StatusBlocked            Status = "BLOCKED"
	StatusSucceeded          Status = "SUCCEEDED"
	StatusFailed             Status = "FAILED"
	StatusPartiallyApplied   Status = "PARTIALLY_APPLIED"
	StatusCompensating       Status = "COMPENSATING"
	StatusCompensated        Status = "COMPENSATED"
	StatusCompensationFailed Status = "COMPENSATION_FAILED"
	StatusCancelRequested    Status = "CANCEL_REQUESTED"
	StatusCancelled          Status = "CANCELLED"
)

// Operation types (control-plane/v1 execution-operation.schema.json).
const (
	// TypeTenantProvisioningApply applies an approved provisioning plan.
	TypeTenantProvisioningApply = "TENANT_PROVISIONING_APPLY"
	// TypeTenantProvisioningRemediate resolves a BLOCKED provisioning's
	// blockers under its approved plan.
	TypeTenantProvisioningRemediate = "TENANT_PROVISIONING_REMEDIATE"
)

var idPattern = regexp.MustCompile(`^op_[a-z0-9]+$`)

// ValidID reports whether id is an operation identifier.
func ValidID(id string) bool { return len(id) >= 6 && len(id) <= 63 && idPattern.MatchString(id) }

// Operation is a durable operation.
type Operation struct {
	ID               string
	Type             string
	Status           Status
	SubjectType      string
	SubjectID        string
	TenantID         string
	PlanID           string
	PlanDigest       string
	ApprovalID       string
	RequestedBy      string
	CurrentPhase     string
	CompletedSteps   *int
	TotalSteps       *int
	CurrentStep      string
	ExecutionAttempt int
	Retryable        bool
	Result           json.RawMessage
	Problem          json.RawMessage
	Revision         int64
	CorrelationID    string
	CreatedAt        time.Time
	StartedAt        *time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}
