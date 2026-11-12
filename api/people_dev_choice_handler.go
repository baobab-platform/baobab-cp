package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

type peopleDevChoiceHandler struct {
	repo       repository.PeopleDevChoiceRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

func peopleDevChoicePermitted(environment string) bool {
	return environment == "development" || environment == "integration"
}

func (h peopleDevChoiceHandler) clock() time.Time {
	if h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

// Parse a closed object and forbid duplicate JSON members and user-supplied
// provider, approver, certification status or employee data.
func parsePeopleDevChoice(raw []byte) (repository.PeopleDevChoiceRequest, bool) {
	var req repository.PeopleDevChoiceRequest
	if len(raw) == 0 || len(raw) > 4096 {
		return req, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return req, false
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		token, err := dec.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return req, false
		}
		switch key {
		case "tenant_id", "legal_employer_id", "jurisdiction", "capability_key",
			"binding_id", "certification_id", "choice_evidence_ref", "effective_from":
		default:
			return req, false
		}
		if _, duplicate := fields[key]; duplicate {
			return req, false
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return req, false
		}
		fields[key] = value
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('}') {
		return req, false
	}
	if _, err := dec.Token(); err != io.EOF || len(fields) != 8 {
		return req, false
	}
	body := append([]byte(nil), raw...)
	strict := json.NewDecoder(bytes.NewReader(body))
	strict.DisallowUnknownFields()
	if strict.Decode(&req) != nil {
		return req, false
	}
	if _, err := strict.Token(); err != io.EOF {
		return req, false
	}
	return req, true
}

func (h peopleDevChoiceHandler) propose(w http.ResponseWriter, r *http.Request) {
	proposer, _, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	req, ok := parsePeopleDevChoice(raw)
	if !ok || req.Validate(h.clock()) != nil {
		problem(w, r, http.StatusBadRequest, "PEOPLE_DEV_CHOICE_INVALID", "Exact employer-scoped proposal required", false)
		return
	}
	record, err := h.repo.ProposePeopleDevChoice(r.Context(), req, proposer, h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/people/provider-choices/"+record.ChoiceID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", entityTag(record.ChoiceVersion))
	writeJSON(w, http.StatusCreated, record)
}

func (h peopleDevChoiceHandler) approve(w http.ResponseWriter, r *http.Request) {
	checker, _, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	choiceID := strings.TrimSpace(chi.URLParam(r, "choiceID"))
	if len(choiceID) != 36 {
		problem(w, r, http.StatusBadRequest, "PEOPLE_DEV_CHOICE_INVALID", "Exact choice UUID required", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var version struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&version) != nil || version.ExpectedVersion < 1 {
		problem(w, r, http.StatusBadRequest, "PEOPLE_DEV_CHOICE_INVALID", "Expected current version required", false)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		problem(w, r, http.StatusBadRequest, "PEOPLE_DEV_CHOICE_INVALID", "Unexpected approval fields", false)
		return
	}
	// No approval identity, vendor credentials or certification assertion is
	// accepted in the request; resolveActor independently authenticates human.
	record, err := h.repo.ApprovePeopleDevChoice(r.Context(), choiceID, version.ExpectedVersion, checker, h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", entityTag(record.ChoiceVersion))
	writeJSON(w, http.StatusOK, record)
}

func (h peopleDevChoiceHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, repository.ErrPeopleDevChoiceConflict) {
		problem(w, r, http.StatusConflict, "PEOPLE_DEV_CHOICE_CONFLICT", "Choice already decided or version changed", false)
		return
	}
	if errors.Is(err, repository.ErrPeopleDevChoiceDenied) {
		problem(w, r, http.StatusForbidden, "PEOPLE_DEV_PROVIDER_UNQUALIFIED", "Current employer/provider/EA-09 authority denied", false)
		return
	}
	problem(w, r, http.StatusServiceUnavailable, "PEOPLE_DEV_CHOICE_UNAVAILABLE", "Development People governance unavailable", true)
}

var _ = strconv.FormatInt // used by other gated API handlers, keep compile parity
