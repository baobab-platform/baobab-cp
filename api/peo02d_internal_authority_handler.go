package api

// PEO-02D: authoritative non-cacheable CP decision for the one INTERNAL
// billing projection named by an authenticated baobab-subscriptions workload.
// Tokens only invoke the PDP. A successful historic classification, event,
// group relationship or legal-entity claim does not confer ongoing authority.
import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/subscription"
	"github.com/go-chi/chi/v5"
)

const internalAuthorityPolicyRef = "ADR-BCP-017/018;PEO-02C;PEO-02D:current-INTERNAL"

type internalAuthorityResponse struct {
	TenantID                string    `json:"tenant_id"`
	ProductSubscriptionID   string    `json:"product_subscription_id"`
	ClassificationReference string    `json:"classification_reference"`
	Eligible                bool      `json:"eligible"`
	EvaluatedAt             time.Time `json:"evaluated_at"`
	ExpiresAt               time.Time `json:"expires_at"`
	PolicyReference         string    `json:"policy_reference"`
}
type internalAuthorityHandler struct {
	classifications *subscription.Classifier
	clientID        string
	now             func() time.Time
}
func (h internalAuthorityHandler) read(w http.ResponseWriter,r *http.Request) {
	w.Header().Set("Cache-Control","no-store, max-age=0")
	w.Header().Set("Pragma","no-cache")
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType!="workload" || h.clientID=="" ||
		principal.ClientID!=h.clientID {
		problem(w,r,http.StatusForbidden,"INTERNAL_AUTHORITY_CALLER_DENIED",
			"the calling workload has no subscription authority",false)
		return
	}
	tenantID,productSubscriptionID:=chi.URLParam(r,"tenantID"),chi.URLParam(r,"subscriptionID")
	ref:=r.URL.Query().Get("classification_reference")
	if !strings.HasPrefix(tenantID,"tn_") || !strings.HasPrefix(productSubscriptionID,"sub_") ||
		len(ref)<3 || len(ref)>128 {
		problem(w,r,http.StatusBadRequest,"INVALID_INTERNAL_AUTHORITY_QUERY",
			"one tenant, ProductSubscription and classification reference are required",false)
		return
	}
	if _,err:=domain.ParseResourceID(domain.ProductSubscriptionIDPrefix,productSubscriptionID);err!=nil {
		problem(w,r,http.StatusBadRequest,"INVALID_PRODUCT_SUBSCRIPTION",
			"the subscription identifier is invalid",false)
		return
	}
	explanation,err:=h.classifications.Explain(r.Context(),productSubscriptionID)
	if errors.Is(err,repository.ErrProductSubscriptionNotFound) ||
		errors.Is(err,subscription.ErrNotFound) ||
		errors.Is(err,subscription.ErrNotClassified) {
		problem(w,r,http.StatusNotFound,"PRODUCT_SUBSCRIPTION_NOT_FOUND",
			"no current classified ProductSubscription",false)
		return
	}
	if err!=nil {
		problem(w,r,http.StatusServiceUnavailable,"CURRENT_AUTHORITY_UNAVAILABLE",
			"the current INTERNAL policy cannot be evaluated",true)
		return
	}
	now:=time.Now().UTC()
	if h.now!=nil { now=h.now().UTC() }
	// Reconcile the actual current primary Organisation; a classification
	// attached to an old tenant/organisation must never be a continuing
	// charge waiver after a tenant-organisation re-point.
	currentPrimary:=""
	primaryCount:=0
	if explanation.Current.InternalEligibility!=nil {
		mappings,e:=h.classifications.Orgs.ListLiveTenantOrganisationMappings(r.Context(),tenantID)
		if e!=nil {
			problem(w,r,http.StatusServiceUnavailable,"CURRENT_AUTHORITY_UNAVAILABLE",
				"the live primary Organisation could not be confirmed",true)
			return
		}
		for _,m:=range mappings {
			if m.MappingRole==domain.TenantOrgRolePrimary && m.Status==domain.RelationshipStatusActive {
				primaryCount++
				currentPrimary=m.OrganisationID
			}
		}
	}
	eligible:=explanation.TenantID==tenantID &&
		explanation.SubscriptionID==productSubscriptionID &&
		explanation.Current.SubscriptionID==productSubscriptionID &&
		explanation.Current.TenantID==tenantID &&
		explanation.Current.SubscriptionType==domain.SubscriptionInternal &&
		explanation.Current.Reference==ref &&
		explanation.Current.InternalEligibility!=nil &&
		explanation.CurrentEligibility!=nil &&
		explanation.CurrentEligibility.EligibilityStatus=="ELIGIBLE" &&
		primaryCount==1 && currentPrimary==explanation.Current.InternalEligibility.OrganisationID
	expires:=now.Add(3*time.Second)
	// A negative result is also transient. The consumer is never permitted
	// to cache TRUE across expiry/revocation; the CP endpoint explicitly
	// forbids HTTP caches and returns the short lease to make misuse visible.
	writeJSON(w,http.StatusOK,internalAuthorityResponse{
		TenantID:tenantID,ProductSubscriptionID:productSubscriptionID,
		ClassificationReference:ref,Eligible:eligible,
		EvaluatedAt:now,ExpiresAt:expires,PolicyReference:internalAuthorityPolicyRef,
	})
}
