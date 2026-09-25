// ADR-BCP-018 gate ORG-11 — ProductSubscription classification API
// (ADR-BCP-017 sections 10-13, 48; ADR-SHARED-011). Contract:
// baobab-platform/shared contracts/product/v1.
//
// Platform administrators with a registered Control Plane principal classify
// (subscription:classify) and read explanations (subscription:read). Nobody
// classifies a subscription of a tenant they work for, nor one they applied
// for.
package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service/subscription"
)

type subscriptionClassificationHandler struct {
	svc        *subscription.Classifier
	identities repository.IdentityRepository
}

func (h subscriptionClassificationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *subscription.InvalidError
	switch {
	case errors.As(err, &invalid):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", strings.Join(invalid.Problems, "; "), false)
	case errors.Is(err, subscription.ErrNotFound):
		problem(w, r, http.StatusNotFound, "PRODUCT_SUBSCRIPTION_NOT_FOUND", "no such product subscription", false)
	case errors.Is(err, subscription.ErrNotClassified):
		problem(w, r, http.StatusConflict, "SUBSCRIPTION_NOT_CLASSIFIED", err.Error(), false)
	case errors.Is(err, subscription.ErrAlreadyClassified):
		problem(w, r, http.StatusConflict, "SUBSCRIPTION_ALREADY_CLASSIFIED", err.Error(), false)
	case errors.Is(err, subscription.ErrUnchangedType):
		problem(w, r, http.StatusConflict, "SUBSCRIPTION_TYPE_UNCHANGED", err.Error(), false)
	case errors.Is(err, subscription.ErrSelfClassification):
		problem(w, r, http.StatusForbidden, "SELF_CLASSIFICATION_FORBIDDEN", err.Error(), false)
	case errors.Is(err, subscription.ErrDecisionNotFound):
		problem(w, r, http.StatusUnprocessableEntity, "ADMISSION_DECISION_NOT_FOUND", err.Error(), false)
	case errors.Is(err, subscription.ErrDecisionNotApproved):
		problem(w, r, http.StatusUnprocessableEntity, "ADMISSION_DECISION_NOT_APPROVED", err.Error(), false)
	case errors.Is(err, subscription.ErrDecisionNotForTenant):
		problem(w, r, http.StatusUnprocessableEntity, "ADMISSION_DECISION_NOT_FOR_TENANT", err.Error(), false)
	case errors.Is(err, subscription.ErrNoPrimaryOrganisation):
		problem(w, r, http.StatusUnprocessableEntity, "TENANT_ORGANISATION_REQUIRED", err.Error(), false)
	case errors.Is(err, subscription.ErrNotInternalEligible):
		problem(w, r, http.StatusUnprocessableEntity, "NOT_INTERNAL_ELIGIBLE", err.Error(), false)
	default:
		// Includes an eligibility evaluation that could not complete: the
		// classification fails closed and the caller may retry.
		problem(w, r, http.StatusServiceUnavailable, "CLASSIFICATION_UNAVAILABLE", "the classification could not be evaluated", true)
	}
}

func (h subscriptionClassificationHandler) command(run func(*subscription.Classifier, *http.Request, subscription.Actor, string, []byte) (any, bool, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principalID, audit, ok := resolveActor(w, r, h.identities, false)
		if !ok {
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		out, created, err := run(h.svc, r, subscription.Actor{PrincipalID: principalID, Audit: audit}, chi.URLParam(r, "subscriptionID"), raw)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, out)
	}
}

func (h subscriptionClassificationHandler) classify() http.HandlerFunc {
	return h.command(func(s *subscription.Classifier, r *http.Request, a subscription.Actor, id string, raw []byte) (any, bool, error) {
		return s.ClassifyFromAdmission(r.Context(), a, id, raw)
	})
}

func (h subscriptionClassificationHandler) reclassify() http.HandlerFunc {
	return h.command(func(s *subscription.Classifier, r *http.Request, a subscription.Actor, id string, raw []byte) (any, bool, error) {
		return s.Reclassify(r.Context(), a, id, raw)
	})
}

func (h subscriptionClassificationHandler) explain(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := resolveActor(w, r, h.identities, false); !ok {
		return
	}
	out, err := h.svc.Explain(r.Context(), chi.URLParam(r, "subscriptionID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h subscriptionClassificationHandler) explainTenantProduct(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := resolveActor(w, r, h.identities, false); !ok {
		return
	}
	out, err := h.svc.ExplainTenantProduct(r.Context(), chi.URLParam(r, "tenantID"), chi.URLParam(r, "productID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
