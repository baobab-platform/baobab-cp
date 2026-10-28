// Package tmsauthority models the still-unaccepted CP-native transport
// execution authority boundary (ADR-TMS-0004/0013/0020/0021, EA v2.3).
//
// Shared proposal #409 is NOT an accepted contract. This package MUST NOT
// mint ALLOW, perform an HTTP handler, persist a decision or activate a
// carrier; its source inputs are review fixtures, not CP repository readbacks.
package tmsauthority

import (
	"strings"
	"time"
)

const (
	StatusHeld = "HELD"
	ContractStatus = "PROPOSED_NOT_ACCEPTED"
)

// Operation belongs to TMS, not to the generic CP provisioning operation enum.
type Operation string

const (
	BookingIntent Operation = "tms.booking.intent.create"
	BookingSubmit Operation = "tms.booking.submit"
	BookingReconcile Operation = "tms.booking.reconcile"
	CapacityHold Operation = "tms.capacity.hold"
	CapacityRelease Operation = "tms.capacity.release"
	MovementDispatch Operation = "tms.movement.dispatch"
	NativeProvision Operation = "tms.native.provision"
	NativeSuspend Operation = "tms.native.suspend"
	NativeDeprovision Operation = "tms.native.deprovision"
)
var operations = map[Operation]bool{
	BookingIntent:true,BookingSubmit:true,BookingReconcile:true,
	CapacityHold:true,CapacityRelease:true,MovementDispatch:true,
	NativeProvision:true,NativeSuspend:true,NativeDeprovision:true,
}

// CanonicalScope cannot be inferred from an untrusted native company,
// vendor driver or caller-supplied tenant header.
type CanonicalScope struct {
	TenantID string
	OperatingOrganisationID string
	LegalEntityID string
	MarketID string
	Environment string
}

// TrustedInput names the independent CP/IAM/current-provider checks an
// eventual owner-operated service must obtain. A struct is NOT itself
// trusted; a forged input cannot turn into an approval in this package.
type TrustedInput struct {
	Scope CanonicalScope
	ActorIssuer string
	ActorSubject string
	PrincipalID string
	WorkloadClientID string
	TokenID string
	IAMCurrent bool
	ContextCurrent bool
	LegalMandateCurrent bool
	ProviderBindingCurrent bool
	ProviderReleaseCurrent bool
	CapabilityCurrent bool
	Operation Operation
	AggregateID string
	IntentDigest string
	BindingID string
	BindingRevision string
	ProviderReleaseDigest string
	FencingGeneration uint64
	LeaseValidUntil time.Time
	LegalHold bool
	NativeProviderRevoked bool
}

// Decision NEVER represents CP authority. Even a fully-populated input is
// HELD until accepted Shared/IAM/CP wire contracts and independent readbacks.
type Decision struct {
	Status string
	ContractStatus string
	Executable bool
	ActivatesProvider bool
	Reasons []string
}

func nonblank(s string) bool { return len(s)>0 && len(s)<=160 && s==strings.TrimSpace(s) }
func digest(s string) bool {
	if len(s)!=71 || !strings.HasPrefix(s,"sha256:"){return false}
	for _, c:=range s[len("sha256:"):] {
		if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}
	}
	return true
}

// AssessCandidate is a non-authorising source/shape preflight. The final
// outcome is never ALLOW; no dependency injection may override this gate.
func AssessCandidate(in TrustedInput,now time.Time) Decision {
	reasons:=make([]string,0,12)
	add:=func(b bool,s string){if b{reasons=append(reasons,s)}}
	add(!operations[in.Operation],"OPERATION_NOT_DEFINED")
	add(!nonblank(in.Scope.TenantID)||!nonblank(in.Scope.OperatingOrganisationID)||
		!nonblank(in.Scope.LegalEntityID)||!nonblank(in.Scope.MarketID)||
		!nonblank(in.Scope.Environment),"CANONICAL_SCOPE_MISSING")
	add(in.Scope.Environment!="development"&&in.Scope.Environment!="staging"&&
		in.Scope.Environment!="production","UNRECOGNISED_ENVIRONMENT")
	add(!in.IAMCurrent||!nonblank(in.ActorIssuer)||!nonblank(in.ActorSubject)||
		!nonblank(in.TokenID)||!nonblank(in.WorkloadClientID)||!nonblank(in.PrincipalID),
		"IAM_IDENTITY_NOT_CURRENT")
	add(!in.ContextCurrent,"CP_CONTEXT_NOT_CURRENT")
	add(!in.LegalMandateCurrent||in.LegalHold,"LEGAL_MANDATE_OR_HOLD")
	add(!in.ProviderBindingCurrent||!in.ProviderReleaseCurrent||in.NativeProviderRevoked,
		"PROVIDER_BINDING_OR_RELEASE_REVOKED")
	add(!in.CapabilityCurrent,"CAPABILITY_NOT_CURRENT")
	add(!nonblank(in.AggregateID)||!digest(in.IntentDigest)||
		!nonblank(in.BindingID)||!nonblank(in.BindingRevision)||
		!digest(in.ProviderReleaseDigest),"RESOURCE_OR_INTENT_NOT_PINNED")
	add(in.FencingGeneration==0||now.IsZero()||
		!in.LeaseValidUntil.After(now)||in.LeaseValidUntil.After(now.Add(30*time.Second)),
		"OPERATION_FENCE_NOT_CURRENT")
	// Even a perfect, fresh and apparently trusted candidate is NOT a CP
	// decision until owners accept and implement the missing authority.
	reasons=append(reasons,"TMS_NATIVE_EXECUTION_CONTRACT_NOT_ACCEPTED")
	return Decision{
		Status:StatusHeld, ContractStatus:ContractStatus,
		Executable:false,ActivatesProvider:false,Reasons:reasons,
	}
}
