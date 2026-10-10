// PEO-03B independent maker/checker admission -> authorised, pre-tenant request.
// This does not convert a v2 application into v1, create a tenant, verify a
// corporation, publish an unregistered event, or issue commercial authority.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

var ErrProgressiveBridgeDenied = errors.New("PEO-03B independent admission authority absent")
var bridgeProductKey = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var bridgeOnboardingSchema = contracts.MustSchema("admission/v2/onboarding.schema.json#/$defs/TenantOnboardingRequestV2")

type ProgressiveAdmissionReviewInput struct {
	OrganisationID string `json:"organisation_id"`
	EvidenceReference string `json:"evidence_reference"`
	IdentityResolutionPolicyReference string `json:"identity_resolution_policy_reference"`
	SubscriptionType string `json:"subscription_type"`
	MarketScope []string `json:"market_scope"`
	ProductRequirements []string `json:"product_requirements"`
	IsolationStrategy string `json:"isolation_strategy"`
}
type ProgressiveAdmissionDecisionInput struct {
	Decision string `json:"decision"`
	Reason string `json:"reason"`
	EvidenceReference string `json:"evidence_reference"`
}
type ProgressiveOnboardingInput struct {
	DisplayName string `json:"display_name"`
	ResidencyRegion string `json:"residency_region"`
	MarketParticipation []domain.OnboardingMarketParticipation `json:"market_participation"`
	Reason string `json:"reason"`
}
type ProgressiveAuthorisationInput struct {
	PolicyReference string `json:"policy_reference"`
	EvidenceReference string `json:"evidence_reference"`
}
type ProgressiveBridgeReceipt struct {
	ResourceID string `json:"resource_id"`
	ApplicationID string `json:"client_application_id,omitempty"`
	OrganisationID string `json:"organisation_id,omitempty"`
	Status string `json:"status"`
}
type progressiveMutation func(pgx.Tx) (ProgressiveBridgeReceipt, error)

func bridgeReference(s string) bool { return len(strings.TrimSpace(s)) >= 3 && len(s) <= 500 }
func bridgeHuman(ctx context.Context, tx pgx.Tx, actor string) (bool, error) {
	var human bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal
		WHERE principal_id=$1::uuid AND actor_type='human')`, actor).Scan(&human)
	return human, err
}
func bridgeOrganisation(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM registry.organisation_profile op
		JOIN registry.canonical_entity ce ON ce.canonical_entity_id=op.canonical_entity_id
		WHERE op.canonical_entity_id=$1::uuid AND op.status='ACTIVE'
		  AND ce.entity_type='ORGANISATION' AND ce.tenant_id IS NULL)`, id).Scan(&ok)
	return ok, err
}
func bridgeScope(values []string, pattern *regexp.Regexp, maximum int) bool {
	if len(values)>maximum { return false }
	seen:=make(map[string]bool,len(values))
	for _,v:=range values {
		if !pattern.MatchString(v) || seen[v] { return false }
		seen[v]=true
	}
	return true
}
var bridgeMarketCode = regexp.MustCompile(`^[A-Z]{2}$`)
func bridgeMarketSubset(approved []string, requested []byte) bool {
	if len(approved)==0 || !bridgeScope(approved,bridgeMarketCode,50) { return false }
	var input []domain.RequestedMarket
	if json.Unmarshal(requested,&input)!=nil { return false }
	offered:=make(map[string]bool,len(input))
	for _,m:=range input { offered[m.CountryCode]=true }
	for _,m:=range approved { if !offered[m] { return false } }
	return true
}
func bridgeParticipants(approved []string, entries []domain.OnboardingMarketParticipation) bool {
	if len(entries)!=len(approved) { return false }
	allow:=make(map[string]bool,len(approved))
	for _,m:=range approved { allow[m]=true }
	for _,p:=range entries {
		if !allow[p.Market] || len(p.Activities)==0 || len(p.Activities)>20 { return false }
		delete(allow,p.Market)
	}
	return len(allow)==0
}

// Recheck the actual authoritative, time-bound sponsorship at the point of
// use. A corporate relationship or an applicant's INTERNAL request is not
// enough. A revoked, suspended, expired or unverified sponsor is ineligible.
func bridgeInternalEligible(ctx context.Context, tx pgx.Tx, org string) (bool,error) {
	var ok bool
	err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM admission.founding_group_sponsorship s
		JOIN registry.first_party_organisation_identity fp ON fp.organisation_id=s.sponsor_organisation_id
		JOIN registry.legal_entity_profile lp ON lp.organisation_id=fp.organisation_id
		WHERE s.operating_organisation_id=$1::uuid AND s.status='ACTIVE'
		  AND s.scope='INTERNAL_GROUP_ADMISSION'
		  AND s.effective_from<=clock_timestamp() AND s.effective_to>clock_timestamp()
		  AND fp.identity_class='LEGAL_PERSON' AND fp.incorporation_claim='REGISTERED_EVIDENCED'
		  AND lp.verification_state='VERIFIED' AND lp.legal_status='ACTIVE'
		  AND lp.verified_at<=clock_timestamp() AND lp.effective_from<=clock_timestamp()
		  AND (lp.effective_to IS NULL OR lp.effective_to>clock_timestamp())
		  AND jsonb_array_length(lp.evidence_references)>0)`,org).Scan(&ok)
	return ok,err
}
func bridgeAudit(ctx context.Context,tx pgx.Tx,meta basestore.RequestMetadata,verb,target string,receipt ProgressiveBridgeReceipt) error {
	raw,err:=json.Marshal(receipt)
	if err!=nil { return err }
	_,err=tx.Exec(ctx,`INSERT INTO audit_events(actor_id,actor_type,client_id,token_id,correlation_id,
 action,target,result,payload) VALUES($1::uuid,$2,NULLIF($3,''),NULLIF($4,''),$5::uuid,$6,$7,'accepted',$8::jsonb)`,
		meta.ActorID,meta.ActorType,meta.ClientID,meta.TokenID,meta.CorrelationID,
		"progressive_admission."+verb,"admission-v2/"+target,raw)
	return err
}
func (s *Store) bridgeCommand(ctx context.Context,meta basestore.RequestMetadata,actor,key,action,target string,
	input any,mutation progressiveMutation) (ProgressiveBridgeReceipt,error) {
	var empty ProgressiveBridgeReceipt
	if requireProgressiveHuman(meta,actor)!=nil || !domain.IsUUID(target) ||
		len(key)<16 || len(key)>128 { return empty,ErrProgressiveBridgeDenied }
	payload,err:=json.Marshal(struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Input any `json:"input"`
	}{action,target,input})
	if err!=nil { return empty,err }
	digest:=fmt.Sprintf("%x",sha256.Sum256(payload))
	tx,err:=s.pool.Begin(ctx)
	if err!=nil { return empty,err }
	defer tx.Rollback(ctx)
	if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo03b:"+actor+":"+key);err!=nil { return empty,err }
	var original,oldAction,oldTarget string
	var receiptData []byte
	err=tx.QueryRow(ctx,`SELECT request_digest,action,target_id::text,receipt
		FROM admission.progressive_bridge_command WHERE actor_id=$1::uuid AND idempotency_key=$2`,
		actor,key).Scan(&original,&oldAction,&oldTarget,&receiptData)
	if err==nil {
		if original!=digest || oldAction!=action || oldTarget!=target { return empty,ErrProgressiveBridgeDenied }
		var previous ProgressiveBridgeReceipt
		if err=json.Unmarshal(receiptData,&previous);err!=nil{return empty,err}
		return previous,nil
	}
	if !errors.Is(err,pgx.ErrNoRows) { return empty,err }
	ok,err:=bridgeHuman(ctx,tx,actor)
	if err!=nil {return empty,err}
	if !ok {return empty,ErrProgressiveBridgeDenied}
	out,err:=mutation(tx)
	if err!=nil { return empty,err }
	if err=bridgeAudit(ctx,tx,meta,strings.ToLower(action),target,out);err!=nil{return empty,err}
	receiptData,err=json.Marshal(out)
	if err!=nil{return empty,err}
	_,err=tx.Exec(ctx,`INSERT INTO admission.progressive_bridge_command
		(actor_id,idempotency_key,action,target_id,request_digest,receipt)
		VALUES($1::uuid,$2,$3,$4::uuid,$5,$6::jsonb)`,
		actor,key,action,target,digest,receiptData)
	if err!=nil {return empty,err}
	if err=tx.Commit(ctx);err!=nil{return empty,err}
	return out,nil
}

// REVIEW does not accept applicant-supplied subscription classification as
// authority; the platform reviewer records an explicit scoped assessment.
func (s *Store) ReviewProgressiveAdmission(ctx context.Context,key string,meta basestore.RequestMetadata,
	reviewer,applicationID string,in ProgressiveAdmissionReviewInput) (ProgressiveBridgeReceipt,error) {
	application,err:=domain.ParseResourceID(domain.ClientApplicationIDPrefix,applicationID)
	if err!=nil {return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
	if !domain.IsUUID(in.OrganisationID) || !bridgeReference(in.EvidenceReference) ||
		!bridgeReference(in.IdentityResolutionPolicyReference) ||
		(in.SubscriptionType!="COMMERCIAL" && in.SubscriptionType!="INTERNAL") ||
		(in.IsolationStrategy!="schema_per_tenant" && in.IsolationStrategy!="row_level_security") ||
		!bridgeScope(in.ProductRequirements,bridgeProductKey,50) ||
		!bridgeScope(in.MarketScope,bridgeMarketCode,50) || len(in.MarketScope)==0 {
		return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied
	}
	return s.bridgeCommand(ctx,meta,reviewer,key,"REVIEW",application,in,func(tx pgx.Tx)(ProgressiveBridgeReceipt,error){
		var applicant,status string
		var requested []byte
		err:=tx.QueryRow(ctx,`SELECT applicant_principal_id::text,status,requested_markets
			FROM admission.client_application_v2 WHERE client_application_id=$1::uuid FOR UPDATE`,application).
			Scan(&applicant,&status,&requested)
		if errors.Is(err,pgx.ErrNoRows) {return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if applicant==reviewer || status!="SUBMITTED" || !bridgeMarketSubset(in.MarketScope,requested){
			return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied
		}
		ok,err:=bridgeOrganisation(ctx,tx,in.OrganisationID)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		// INTERNAL classification requires positive CURRENT sponsorship already
		// at assessment, as well as at decision and authorisation.
		if in.SubscriptionType=="INTERNAL" {
			ok,err=bridgeInternalEligible(ctx,tx,in.OrganisationID)
			if err!=nil{return ProgressiveBridgeReceipt{},err}
			if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		}
		id:=domain.NewUUIDv7()
		_,err=tx.Exec(ctx,`INSERT INTO admission.progressive_admission_review
			(review_id,client_application_id,organisation_id,reviewed_by,evidence_reference,
			 identity_resolution_policy_reference,approved_subscription_type,approved_market_scope,
			 approved_product_requirements,approved_isolation_strategy)
			VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10)`,
			id,application,in.OrganisationID,reviewer,in.EvidenceReference,
			in.IdentityResolutionPolicyReference,in.SubscriptionType,in.MarketScope,
			in.ProductRequirements,in.IsolationStrategy)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		return ProgressiveBridgeReceipt{ResourceID:id,ApplicationID:applicationID,OrganisationID:in.OrganisationID,Status:"REVIEWED"},nil
	})
}

// DECIDE requires a different independently authorised human; the review is
// frozen. REJECTED never creates an onboarding request.
func (s *Store) DecideProgressiveAdmission(ctx context.Context,key string,meta basestore.RequestMetadata,
	decider,reviewID string,in ProgressiveAdmissionDecisionInput) (ProgressiveBridgeReceipt,error) {
	if !domain.IsUUID(reviewID) || (in.Decision!="APPROVED" && in.Decision!="REJECTED") ||
		len(strings.TrimSpace(in.Reason))<10 || len(in.Reason)>2000 || !bridgeReference(in.EvidenceReference) {
		return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied
	}
	return s.bridgeCommand(ctx,meta,decider,key,"DECIDE",reviewID,in,func(tx pgx.Tx)(ProgressiveBridgeReceipt,error){
		var application,applicant,reviewer,org,subscription string
		err:=tx.QueryRow(ctx,`SELECT r.client_application_id::text,a.applicant_principal_id::text,
		  r.reviewed_by::text,r.organisation_id::text,r.approved_subscription_type
		  FROM admission.progressive_admission_review r
		  JOIN admission.client_application_v2 a ON a.client_application_id=r.client_application_id
		  WHERE r.review_id=$1::uuid AND a.status='SUBMITTED' FOR SHARE OF r,a`,reviewID).
			Scan(&application,&applicant,&reviewer,&org,&subscription)
		if errors.Is(err,pgx.ErrNoRows){return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if decider==reviewer || decider==applicant {return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		ok,err:=bridgeOrganisation(ctx,tx,org)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if in.Decision=="APPROVED" && subscription=="INTERNAL" {
			ok,err=bridgeInternalEligible(ctx,tx,org)
			if err!=nil{return ProgressiveBridgeReceipt{},err}
			if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		}
		id:=domain.NewUUIDv7()
		_,err=tx.Exec(ctx,`INSERT INTO admission.progressive_admission_decision
		  (decision_id,review_id,client_application_id,decision,reason,decision_evidence_reference,decided_by)
		  VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid)`,
		  id,reviewID,application,in.Decision,in.Reason,in.EvidenceReference,decider)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		resource,err:=domain.FormatResourceID(domain.AdmissionDecisionIDPrefix,id)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		appID,err:=domain.FormatResourceID(domain.ClientApplicationIDPrefix,application)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		return ProgressiveBridgeReceipt{ResourceID:resource,ApplicationID:appID,OrganisationID:org,Status:in.Decision},nil
	})
}

type ProgressiveOnboardingV2 struct {
	ID string `json:"tenant_onboarding_request_id"`
	ApplicationID string `json:"client_application_id"`
	DecisionID string `json:"admission_decision_id"`
	OrganisationID string `json:"organisation_id"`
	Status string `json:"status"`
	DesiredState domain.OnboardingDesiredState `json:"desired_state"`
	Reason string `json:"reason"`
	CorrelationID string `json:"correlation_id"`
	RequestedBy string `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
	IdentityPolicyReference string `json:"identity_resolution_policy_reference"`
	AuthorisedBy string `json:"authorised_by,omitempty"`
	AuthorisedAt *time.Time `json:"authorised_at,omitempty"`
	AuthorisationPolicyReference string `json:"authorisation_policy_reference,omitempty"`
}

func (s *Store) RequestProgressiveOnboarding(ctx context.Context,key string,meta basestore.RequestMetadata,
	requester,decisionID string,in ProgressiveOnboardingInput) (ProgressiveBridgeReceipt,error) {
	decision,err:=domain.ParseResourceID(domain.AdmissionDecisionIDPrefix,decisionID)
	if err!=nil || len(strings.TrimSpace(in.Reason))<10 || len(in.Reason)>2000 ||
		len(strings.TrimSpace(in.DisplayName))==0 || !bridgeReference(in.ResidencyRegion) {
		return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied
	}
	return s.bridgeCommand(ctx,meta,requester,key,"REQUEST",decision,in,func(tx pgx.Tx)(ProgressiveBridgeReceipt,error){
		var application,applicant,reviewer,decider,org,subscription,isolation,policy string
		var markets,products []string
		err:=tx.QueryRow(ctx,`SELECT d.client_application_id::text,a.applicant_principal_id::text,
			r.reviewed_by::text,d.decided_by::text,r.organisation_id::text,
			r.approved_subscription_type,r.approved_isolation_strategy,
			r.identity_resolution_policy_reference,r.approved_market_scope,r.approved_product_requirements
			FROM admission.progressive_admission_decision d
			JOIN admission.progressive_admission_review r ON r.review_id=d.review_id
			JOIN admission.client_application_v2 a ON a.client_application_id=d.client_application_id
			WHERE d.decision_id=$1::uuid AND d.decision='APPROVED' AND a.status='SUBMITTED'
			FOR SHARE OF d,r,a`,decision).
			Scan(&application,&applicant,&reviewer,&decider,&org,&subscription,&isolation,&policy,&markets,&products)
		if errors.Is(err,pgx.ErrNoRows){return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if requester==applicant || requester==reviewer || requester==decider ||
			!bridgeParticipants(markets,in.MarketParticipation) {return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		ok,err:=bridgeOrganisation(ctx,tx,org)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if subscription=="INTERNAL" {
			ok,err=bridgeInternalEligible(ctx,tx,org)
			if err!=nil{return ProgressiveBridgeReceipt{},err}
			if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		}
		if products==nil{products=[]string{}}
		desired:=domain.OnboardingDesiredState{
			DisplayName:in.DisplayName,ResidencyRegion:in.ResidencyRegion,
			IsolationStrategy:isolation,SubscriptionType:domain.SubscriptionType(subscription),
			MarketScope:markets,MarketParticipation:in.MarketParticipation,
			ProductRequirements:products,
		}
		id:=domain.NewUUIDv7()
		resource,err:=domain.FormatResourceID(domain.TenantOnboardingRequestIDPrefix,id)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		appID,err:=domain.FormatResourceID(domain.ClientApplicationIDPrefix,application)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		now:=time.Now().UTC()
		onb:=ProgressiveOnboardingV2{
			ID:resource,ApplicationID:appID,DecisionID:decisionID,OrganisationID:org,
			Status:"REQUESTED",DesiredState:desired,Reason:in.Reason,
			CorrelationID:meta.CorrelationID,RequestedBy:requester,RequestedAt:now,
			IdentityPolicyReference:policy,
		}
		if err=contracts.ValidateValue(bridgeOnboardingSchema,onb);err!=nil {
			return ProgressiveBridgeReceipt{},fmt.Errorf("%w: invalid Shared v2 onboarding shape: %v",ErrProgressiveBridgeDenied,err)
		}
		raw,err:=json.Marshal(desired)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		_,err=tx.Exec(ctx,`INSERT INTO admission.progressive_onboarding_request
			(request_id,decision_id,client_application_id,organisation_id,desired_state,
			 identity_resolution_policy_reference,reason,correlation_id,requested_by,requested_at)
			VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::jsonb,$6,$7,$8::uuid,$9::uuid,$10)`,
			id,decision,application,org,raw,policy,in.Reason,meta.CorrelationID,requester,now)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		return ProgressiveBridgeReceipt{ResourceID:resource,ApplicationID:appID,OrganisationID:org,Status:"REQUESTED"},nil
	})
}

// AUTHORISE preserves the Reviewed -> Decided -> Requested -> Authorised
// separation. Only the request status changes; no tenant is created.
func (s *Store) AuthoriseProgressiveOnboarding(ctx context.Context,key string,meta basestore.RequestMetadata,
	authoriser,requestID string,in ProgressiveAuthorisationInput) (ProgressiveBridgeReceipt,error) {
	request,err:=domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix,requestID)
	if err!=nil || !bridgeReference(in.PolicyReference) || !bridgeReference(in.EvidenceReference) {
		return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied
	}
	return s.bridgeCommand(ctx,meta,authoriser,key,"AUTHORISE",request,in,func(tx pgx.Tx)(ProgressiveBridgeReceipt,error){
		var requester,applicant,reviewer,decider,org,application,subscription,status string
		err:=tx.QueryRow(ctx,`SELECT o.requested_by::text,a.applicant_principal_id::text,
			r.reviewed_by::text,d.decided_by::text,o.organisation_id::text,
			o.client_application_id::text,r.approved_subscription_type,o.status
			FROM admission.progressive_onboarding_request o
			JOIN admission.progressive_admission_decision d ON d.decision_id=o.decision_id
			JOIN admission.progressive_admission_review r ON r.review_id=d.review_id
			JOIN admission.client_application_v2 a ON a.client_application_id=o.client_application_id
			WHERE o.request_id=$1::uuid AND d.decision='APPROVED' AND a.status='SUBMITTED'
			FOR UPDATE OF o`,request).
			Scan(&requester,&applicant,&reviewer,&decider,&org,&application,&subscription,&status)
		if errors.Is(err,pgx.ErrNoRows){return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if status!="REQUESTED" || authoriser==requester || authoriser==decider ||
			authoriser==reviewer || authoriser==applicant {
			return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied
		}
		ok,err:=bridgeOrganisation(ctx,tx,org)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		if subscription=="INTERNAL" {
			ok,err=bridgeInternalEligible(ctx,tx,org)
			if err!=nil{return ProgressiveBridgeReceipt{},err}
			if !ok{return ProgressiveBridgeReceipt{},ErrProgressiveBridgeDenied}
		}
		_,err=tx.Exec(ctx,`UPDATE admission.progressive_onboarding_request
			SET status='AUTHORISED',authorised_by=$2::uuid,authorised_at=clock_timestamp(),
			authorisation_policy_reference=$3,authorisation_evidence_reference=$4
			WHERE request_id=$1::uuid AND status='REQUESTED'`,request,authoriser,
			in.PolicyReference,in.EvidenceReference)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		appID,err:=domain.FormatResourceID(domain.ClientApplicationIDPrefix,application)
		if err!=nil{return ProgressiveBridgeReceipt{},err}
		return ProgressiveBridgeReceipt{ResourceID:requestID,ApplicationID:appID,OrganisationID:org,Status:"AUTHORISED"},nil
	})
}
