// PEO-02 downstream PEP: no cached event, relationship or applicant claim
// establishes a current founding-group sponsorship.
package postgres

import (
	"context"
	"time"
)

// FoundingOperatingEligibility distinguishes a first-party operating business
// (where governed sponsorship is required) from all other organisations
// (where this one policy does not apply). Infrastructure failure is never
// interpreted as ineligibility or permission.
func (s *Store) FoundingOperatingEligibility(ctx context.Context, organisationID string, at time.Time) (applies, eligible bool, err error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM registry.first_party_organisation_identity
		WHERE organisation_id=$1::uuid AND identity_class='OPERATING_BUSINESS'
	)`, organisationID).Scan(&applies)
	if err != nil || !applies {
		return applies, false, err
	}
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(
	  SELECT 1 FROM admission.founding_group_sponsorship s
	  JOIN registry.first_party_organisation_identity fp ON fp.organisation_id=s.sponsor_organisation_id
	  JOIN registry.legal_entity_profile lp ON lp.organisation_id=fp.organisation_id
	  WHERE s.operating_organisation_id=$1::uuid
	    AND s.scope='INTERNAL_GROUP_ADMISSION' AND s.status='ACTIVE'
	    AND s.effective_from<=$2 AND s.effective_to>$2
	    AND fp.identity_class='LEGAL_PERSON' AND fp.incorporation_claim='REGISTERED_EVIDENCED'
	    AND lp.verification_state='VERIFIED' AND lp.legal_status='ACTIVE'
	    AND lp.source_authority NOT IN ('shared-governance','control-plane-registration')
	    AND lp.verified_at<=$2 AND lp.effective_from<=$2
	    AND (lp.effective_to IS NULL OR lp.effective_to>$2)
	    AND jsonb_array_length(lp.evidence_references)>0
	)`, organisationID, at).Scan(&eligible)
	return applies, eligible, err
}
