// ADR-BCP-023 sections 7, 9 and 191-192: an applicant's claims on their own
// client application. Contract: baobab-platform/shared
// contracts/control-plane/v1/openapi.yaml (createApplicantClaim,
// listApplicantClaims, withdrawApplicantClaim) and
// contracts/evidence/v1/evidence.schema.json ApplicantClaimSubmission.
//
// The applicant supplies a claim type, jurisdiction and value only. The
// application, its admission case, the asserter, the origin and the
// SELF_ASSERTED status are derived; an applicant never names a case or a
// subject and never verifies anything.
package api

import (
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

var (
	applicantClaimSchema = contracts.MustSchema("evidence/v1/evidence.schema.json#/$defs/ApplicantClaimSubmission")
	claimIDPattern       = regexp.MustCompile(`^ecl_[a-z0-9]+$`)
)

type applicantClaimHandler struct {
	apps   clientApplicationHandler
	claims repository.ApplicantClaims
	now    func() time.Time
}

func (h applicantClaimHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

// ownApplication resolves the applicant and their application; anyone
// else's application is not found.
func (h applicantClaimHandler) ownApplication(w http.ResponseWriter, r *http.Request) (application.Actor, domain.ClientApplication, bool) {
	actor, ok := h.apps.actor(w, r, true)
	if !ok {
		return actor, domain.ClientApplication{}, false
	}
	app, err := h.apps.svc.GetForApplicant(r.Context(), actor, chi.URLParam(r, "applicationID"))
	if err != nil {
		h.apps.fail(w, r, err)
		return actor, app, false
	}
	return actor, app, true
}

func (h applicantClaimHandler) create(w http.ResponseWriter, r *http.Request) {
	actor, app, ok := h.ownApplication(w, r)
	if !ok {
		return
	}
	if !app.Status.EditableByApplicant() {
		problem(w, r, http.StatusConflict, "APPLICATION_NOT_EDITABLE", "claims can be added only while the application is DRAFT or INFORMATION_REQUIRED", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var claim verification.ApplicantClaim
	if !decodeRaw(w, r, applicantClaimSchema, raw, &claim) {
		return
	}
	cl, err := h.claims.AddApplicantClaim(r.Context(), app.ID, claim, domain.NewResourceID("ecl"), domain.NewResourceID("vcase"),
		actor.PrincipalID, h.clock(), actor.Audit)
	if err != nil {
		verificationHandler{}.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(cl.Version))
	writeJSON(w, http.StatusCreated, cl)
}

func (h applicantClaimHandler) list(w http.ResponseWriter, r *http.Request) {
	_, app, ok := h.ownApplication(w, r)
	if !ok {
		return
	}
	claims, err := h.claims.ListApplicationClaims(r.Context(), app.ID)
	if err != nil {
		verificationHandler{}.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemsPage[verification.Claim]{Items: claims})
}

func (h applicantClaimHandler) withdraw(w http.ResponseWriter, r *http.Request) {
	actor, app, ok := h.ownApplication(w, r)
	if !ok {
		return
	}
	claimID, ok := pathID(w, r, "claimID", claimIDPattern)
	if !ok {
		return
	}
	version, ok := versionIfMatch(w, r)
	if !ok {
		return
	}
	cl, err := h.claims.WithdrawApplicantClaim(r.Context(), app.ID, claimID, version, actor.PrincipalID, h.clock(), actor.Audit)
	if err != nil {
		verificationHandler{}.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(cl.Version))
	writeJSON(w, http.StatusOK, cl)
}
