// P0-04B Payments (ADR-SHARED-035): per-movement legal authority. CP decides
// whether the responsible legal entity, operating Organisation and mandate may
// undertake ONE catalogued money movement for ONE canonical instruction
// fingerprint and attempt. Payments independently decides whether the movement
// is financially valid and safe; neither layer overrides the other's denial.
// An AUTHORIZED decision is a 30-second CP fact, never a provider, financial
// approval, readiness or accounting grant.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const (
	paymentsMoneyMovementScope  = "payments-money-movement-authority:resolve"
	paymentsMoneyMovementPolicy = "ADR-SHARED-035/payments-money-movement-authority:v1"
	paymentsNotActivated        = "OPERATION_NOT_ACTIVATED"
	paymentsInstructionMismatch = "INSTRUCTION_MISMATCH"
	paymentsEngineNotBound      = "ENGINE_INSTANCE_NOT_BOUND"
	paymentsContextNotUsable    = "CONTEXT_NOT_USABLE"
	paymentsMoneyMovementLease  = 30 * time.Second
)

type paymentsMoneyMovementStore interface {
	ResolveOperatingLegalActor(context.Context, legalactor.Request) (legalactor.Resolution, error)
	PaymentsEngineInstanceBound(ctx context.Context, tenantID, engineInstanceKey string, at time.Time) (bool, error)
	RecordPaymentsMoneyMovementDecision(context.Context, postgres.PaymentsMoneyMovementDecision) (string, error)
}

// paymentsInstruction is the canonical instruction Payments computed from
// server-verified values. Its context fields are echoes CP checks, never
// selections.
type paymentsInstruction struct {
	TenantID                 string  `json:"tenant_id"`
	OperatingOrganisationID  string  `json:"operating_organisation_id"`
	Market                   string  `json:"market"`
	Currency                 string  `json:"currency"`
	AmountMinor              int64   `json:"amount_minor"`
	PaymentReference         string  `json:"payment_reference"`
	ParentReference          *string `json:"parent_reference"`
	MerchantProfileReference string  `json:"merchant_profile_reference"`
	DestinationReference     *string `json:"destination_reference"`
}

type paymentsMoneyMovementRequest struct {
	ContextID              string              `json:"context_id"`
	EngineInstanceID       string              `json:"engine_instance_id"`
	OperationClass         string              `json:"operation_class"`
	OperationReference     string              `json:"operation_reference"`
	Attempt                int                 `json:"attempt"`
	Instruction            paymentsInstruction `json:"instruction"`
	InstructionFingerprint string              `json:"instruction_fingerprint"`
}

type paymentsMoneyMovementDecision struct {
	DecisionID                 string     `json:"decision_id"`
	ContextID                  string     `json:"context_id"`
	EngineInstanceID           string     `json:"engine_instance_id"`
	OperationClass             string     `json:"operation_class"`
	OperationReference         string     `json:"operation_reference"`
	Attempt                    int        `json:"attempt"`
	InstructionFingerprint     string     `json:"instruction_fingerprint"`
	Outcome                    string     `json:"outcome"`
	TenantID                   string     `json:"tenant_id,omitempty"`
	OperatingOrganisationID    string     `json:"operating_organisation_id,omitempty"`
	Market                     string     `json:"market,omitempty"`
	LegalActorRole             string     `json:"legal_actor_role,omitempty"`
	Activity                   string     `json:"activity"`
	CapabilityKey              string     `json:"capability_key,omitempty"`
	MandateID                  string     `json:"mandate_id,omitempty"`
	SupersedesMandateID        string     `json:"supersedes_mandate_id,omitempty"`
	ResponsibleLegalEntityID   string     `json:"responsible_legal_entity_id,omitempty"`
	EvidenceReferences         []string   `json:"evidence_references,omitempty"`
	MandateEffectiveFrom       *time.Time `json:"mandate_effective_from,omitempty"`
	MandateEffectiveTo         *time.Time `json:"mandate_effective_to,omitempty"`
	EvaluatedAt                time.Time  `json:"evaluated_at"`
	ExpiresAt                  *time.Time `json:"expires_at,omitempty"`
	PolicyReference            string     `json:"policy_reference"`
	ProviderPermissionsGranted bool       `json:"provider_permissions_granted"`
}

var (
	paymentsMoneyMovementRequestSchema = contracts.MustSchema(
		"payments/v2/money-movement-authority.schema.json#/$defs/MoneyMovementAuthorityRequest")
	paymentsMoneyMovementDecisionSchema = contracts.MustSchema(
		"payments/v2/money-movement-authority.schema.json#/$defs/MoneyMovementAuthorityDecision")
)

// paymentsMoneyMovementOperation is one entry of the pinned payments/v2
// catalogue: the ONLY source of the role, activity and capability a class
// requires, and of whether it can be authorised at all.
type paymentsMoneyMovementOperation struct {
	OperationClass    string  `yaml:"operation_class"`
	Role              *string `yaml:"legal_actor_role"`
	Activity          string  `yaml:"activity"`
	CapabilityKey     *string `yaml:"capability_key"`
	ProviderOperation string  `yaml:"provider_operation"`
	Activatable       bool    `yaml:"activatable"`
}

var (
	paymentsCatalogueOnce sync.Once
	paymentsCatalogue     map[string]paymentsMoneyMovementOperation
	paymentsCatalogueErr  error
)

func paymentsMoneyMovementCatalogue() (map[string]paymentsMoneyMovementOperation, error) {
	paymentsCatalogueOnce.Do(func() {
		raw, err := contracts.ReadEmbedded("payments/v2/money-movement-operations.yaml")
		if err != nil {
			paymentsCatalogueErr = err
			return
		}
		var doc struct {
			Operations []paymentsMoneyMovementOperation `yaml:"operations"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			paymentsCatalogueErr = err
			return
		}
		out := make(map[string]paymentsMoneyMovementOperation, len(doc.Operations))
		for _, op := range doc.Operations {
			// An activatable class must name its role and capability; a
			// non-activatable one must name neither, so it can never match.
			valid := op.OperationClass != "" && op.Activity != "" &&
				(op.Activatable == (op.Role != nil && *op.Role != "" && op.CapabilityKey != nil && *op.CapabilityKey != ""))
			if _, dup := out[op.OperationClass]; dup || !valid ||
				(!op.Activatable && (op.Role != nil || op.CapabilityKey != nil)) {
				paymentsCatalogueErr = fmt.Errorf("invalid Payments money-movement catalogue entry %q", op.OperationClass)
				return
			}
			out[op.OperationClass] = op
		}
		if len(out) == 0 {
			paymentsCatalogueErr = fmt.Errorf("empty Payments money-movement catalogue")
			return
		}
		paymentsCatalogue = out
	})
	return paymentsCatalogue, paymentsCatalogueErr
}

// paymentsInstructionFingerprint is the canonical encoding of
// contracts/payments/v2/README.md: the eleven fields with sorted keys, no
// insignificant whitespace, null for an absent reference, SHA-256 hex. The
// schema restricts every string to printable ASCII or fixed patterns, so
// encoding/json without HTML escaping produces the same bytes as Shared.
func paymentsInstructionFingerprint(operationClass, operationReference string, in paymentsInstruction) (string, error) {
	doc := map[string]any{
		"amount_minor":               in.AmountMinor,
		"currency":                   in.Currency,
		"destination_reference":      in.DestinationReference,
		"market":                     in.Market,
		"merchant_profile_reference": in.MerchantProfileReference,
		"operating_organisation_id":  in.OperatingOrganisationID,
		"operation_class":            operationClass,
		"operation_reference":        operationReference,
		"parent_reference":           in.ParentReference,
		"payment_reference":          in.PaymentReference,
		"tenant_id":                  in.TenantID,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	return hex.EncodeToString(sum[:]), nil
}

type paymentsMoneyMovementHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	store      paymentsMoneyMovementStore
	clientIDs  map[string]bool
}

func (h paymentsMoneyMovementHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" || !h.clientIDs[p.ClientID] ||
		!p.HasScope(paymentsMoneyMovementScope) || h.api == nil ||
		h.api.workloadRegistry == nil || !h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, "PAYMENTS_MONEY_MOVEMENT_CALLER_DENIED",
			"active, registered and allowlisted Payments money-movement PEP required", false)
		return
	}
	// Both scopes must be live in the registry, not merely present in a token
	// that outlived a registry revocation: the context this request redeems
	// is itself a context:resolve grant.
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, paymentsMoneyMovementScope) ||
		!scopes.AllowsScope(p.ClientID, "context:resolve") {
		problem(w, r, http.StatusForbidden, "PAYMENTS_MONEY_MOVEMENT_CALLER_DENIED",
			"workload registration does not allow Payments money-movement resolution", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req paymentsMoneyMovementRequest
	if !decodeRaw(w, r, paymentsMoneyMovementRequestSchema, raw, &req) {
		return
	}
	catalogue, err := paymentsMoneyMovementCatalogue()
	if err != nil || h.store == nil || h.contexts == nil || h.identities == nil || h.api.store == nil {
		problem(w, r, http.StatusServiceUnavailable, "PAYMENTS_MONEY_MOVEMENT_UNAVAILABLE",
			"Payments money-movement resolution unavailable", true)
		return
	}
	operation, ok := catalogue[req.OperationClass]
	if !ok {
		problem(w, r, http.StatusBadRequest, "PAYMENTS_MONEY_MOVEMENT_OPERATION_UNKNOWN",
			"operation class is not catalogued", false)
		return
	}
	now := time.Now().UTC()
	decision := paymentsMoneyMovementDecision{
		ContextID: req.ContextID, EngineInstanceID: req.EngineInstanceID,
		OperationClass: req.OperationClass, OperationReference: req.OperationReference,
		Attempt: req.Attempt, InstructionFingerprint: req.InstructionFingerprint,
		Activity: operation.Activity, EvaluatedAt: now, PolicyReference: paymentsMoneyMovementPolicy,
	}
	// Collection authority is never disbursement authority: a class the
	// catalogue does not activate is refused before any context or mandate is
	// read, so no mandate can ever make it AUTHORIZED.
	if !operation.Activatable {
		decision.Outcome = paymentsNotActivated
		h.respond(w, r, p.ClientID, decision)
		return
	}
	// Owner, audience and authenticated-tenant enforcement are CP's own.
	trusted, ok := redeemContext(w, r, h.contexts, h.identities, req.ContextID)
	if !ok {
		return
	}
	decision.ContextID = trusted.ID
	usable := trusted.IsRuntime() && trusted.ExpiresAt != nil && now.Before(*trusted.ExpiresAt) &&
		!trusted.ResolvedAt.After(now) && domain.IsUUID(trusted.OrganisationID) &&
		len(trusted.CountryCode) == 2
	if usable {
		tenant, err := h.api.store.GetTenant(r.Context(), trusted.TenantID)
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "PAYMENTS_MONEY_MOVEMENT_UNAVAILABLE",
				"tenant status could not be verified", true)
			return
		}
		usable = tenant.ObservedState == string(domain.LifecycleActive)
	}
	if !usable {
		decision.Outcome = paymentsContextNotUsable
		h.respond(w, r, p.ClientID, decision)
		return
	}
	// Scope derived only from the trusted context and the pinned catalogue.
	decision.TenantID = trusted.TenantID
	decision.OperatingOrganisationID = trusted.OrganisationID
	decision.Market = trusted.CountryCode
	decision.LegalActorRole = *operation.Role
	decision.CapabilityKey = *operation.CapabilityKey
	// The instruction must echo the context exactly, and its fingerprint must
	// recompute: a decision is bound to these exact financial values.
	fingerprint, err := paymentsInstructionFingerprint(req.OperationClass, req.OperationReference, req.Instruction)
	if err != nil || fingerprint != req.InstructionFingerprint ||
		req.Instruction.TenantID != trusted.TenantID ||
		req.Instruction.OperatingOrganisationID != trusted.OrganisationID ||
		req.Instruction.Market != trusted.CountryCode {
		decision.Outcome = paymentsInstructionMismatch
		h.respond(w, r, p.ClientID, decision)
		return
	}
	bound, err := h.store.PaymentsEngineInstanceBound(r.Context(), trusted.TenantID, req.EngineInstanceID, now)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PAYMENTS_MONEY_MOVEMENT_UNAVAILABLE",
			"engine-instance binding could not be verified", true)
		return
	}
	if !bound {
		decision.Outcome = paymentsEngineNotBound
		h.respond(w, r, p.ClientID, decision)
		return
	}
	resolution, err := h.store.ResolveOperatingLegalActor(r.Context(), legalactor.Request{
		TenantID: trusted.TenantID, OperatingOrganisationID: trusted.OrganisationID,
		Role: *operation.Role, Activity: operation.Activity, Market: trusted.CountryCode,
		Capability: *operation.CapabilityKey, EffectiveAt: now,
	})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PAYMENTS_MONEY_MOVEMENT_UNAVAILABLE",
			"current legal actor authority could not be verified", true)
		return
	}
	decision.Outcome = string(resolution.Outcome)
	if resolution.Outcome == legalactor.Authorized {
		if resolution.ValidUntil == nil || resolution.MandateID == "" ||
			resolution.ResponsibleLegalEntityID == "" || len(resolution.EvidenceReferences) == 0 ||
			resolution.MandateEffectiveFrom == nil {
			decision.Outcome = string(legalactor.ActorNotVerified)
		} else {
			// Never longer than 30 s, the context or the mandate.
			until := *resolution.ValidUntil
			if limit := now.Add(paymentsMoneyMovementLease); limit.Before(until) {
				until = limit
			}
			if trusted.ExpiresAt.Before(until) {
				until = *trusted.ExpiresAt
			}
			if !until.After(now) {
				decision.Outcome = string(legalactor.RevokedOrExpired)
			} else {
				decision.MandateID = resolution.MandateID
				decision.SupersedesMandateID = resolution.SupersedesMandateID
				decision.ResponsibleLegalEntityID = resolution.ResponsibleLegalEntityID
				decision.EvidenceReferences = resolution.EvidenceReferences
				decision.MandateEffectiveFrom = resolution.MandateEffectiveFrom
				decision.MandateEffectiveTo = resolution.MandateEffectiveTo
				decision.ExpiresAt = &until
			}
		}
	}
	h.respond(w, r, p.ClientID, decision)
}

// respond records the decision before disclosing it. An unrecorded decision
// is never returned, so every AUTHORIZED money movement has CP-side history.
func (h paymentsMoneyMovementHandler) respond(w http.ResponseWriter, r *http.Request, clientID string,
	decision paymentsMoneyMovementDecision) {
	decision.ProviderPermissionsGranted = false
	id, err := h.store.RecordPaymentsMoneyMovementDecision(r.Context(), postgres.PaymentsMoneyMovementDecision{
		ClientID: clientID, ContextID: decision.ContextID, EngineInstanceID: decision.EngineInstanceID,
		OperationClass: decision.OperationClass, OperationReference: decision.OperationReference,
		Attempt: decision.Attempt, InstructionFingerprint: decision.InstructionFingerprint,
		Outcome: decision.Outcome, TenantID: decision.TenantID,
		OperatingOrganisationID: decision.OperatingOrganisationID, Market: decision.Market,
		LegalActorRole: decision.LegalActorRole, Activity: decision.Activity, CapabilityKey: decision.CapabilityKey,
		MandateID: decision.MandateID, SupersedesMandateID: decision.SupersedesMandateID,
		ResponsibleLegalEntityID: decision.ResponsibleLegalEntityID,
		EvidenceReferences:       decision.EvidenceReferences,
		MandateEffectiveFrom:     decision.MandateEffectiveFrom, MandateEffectiveTo: decision.MandateEffectiveTo,
		EvaluatedAt: decision.EvaluatedAt, ExpiresAt: decision.ExpiresAt,
		PolicyReference: decision.PolicyReference, CorrelationID: correlationID(r),
	})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PAYMENTS_MONEY_MOVEMENT_UNAVAILABLE",
			"decision could not be recorded", true)
		return
	}
	decision.DecisionID = id
	if err := contracts.ValidateValue(paymentsMoneyMovementDecisionSchema, decision); err != nil {
		// A response that breaks the pinned contract must never reach Payments.
		problem(w, r, http.StatusInternalServerError, "PAYMENTS_MONEY_MOVEMENT_INVALID",
			"decision does not satisfy the pinned contract", false)
		return
	}
	slog.InfoContext(r.Context(), "payments money-movement authority",
		"client_id", clientID, "decision_id", id, "tenant_id", decision.TenantID,
		"context_id", decision.ContextID, "operation_class", decision.OperationClass,
		"operation_reference", decision.OperationReference, "attempt", decision.Attempt,
		"outcome", decision.Outcome, "correlation_id", correlationID(r))
	writeJSON(w, http.StatusOK, decision)
}
