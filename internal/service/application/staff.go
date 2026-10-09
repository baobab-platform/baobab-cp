package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ErrApplicantNotRegistered is a fail-closed refusal: staff-created applications
// belong to a real, already registered human principal, not unchecked input.
var ErrApplicantNotRegistered = errors.New("applicant principal is not an active registered human")

var staffCreateSchema = contracts.MustSchema("admission/v1/application.schema.json#/$defs/StaffApplicationCreateRequest")

type staffCreateRequest struct {
	Channel              domain.ApplicationChannel `json:"application_channel"`
	ApplicantPrincipalID string                    `json:"applicant_principal_id"`
	Reason               string                    `json:"reason"`
	Draft                json.RawMessage           `json:"draft"`
}

// CreateForStaff opens a DRAFT only. It cannot verify evidence or classify
// subscriptions. The applicant (not the operator) owns subsequent responses;
// the operator is separately retained as maker for immutable decision checks.
func (s *Service) CreateForStaff(ctx context.Context, operator Actor, raw []byte, key string) (domain.ClientApplication, bool, error) {
	var req staffCreateRequest
	if err := decode(staffCreateSchema, raw, &req); err != nil {
		return domain.ClientApplication{}, false, err
	}
	var d draft
	if err := decode(draftSchema, req.Draft, &d); err != nil {
		return domain.ClientApplication{}, false, err
	}
	if d.Version != nil {
		return domain.ClientApplication{}, false, &InvalidError{Problems: []string{"/draft/version: new applications cannot supply a version"}}
	}
	if s.Principals == nil {
		return domain.ClientApplication{}, false, errors.New("staff admission requires an identity authority")
	}
	applicant, err := s.Principals.GetPrincipal(ctx, req.ApplicantPrincipalID)
	if errors.Is(err, repository.ErrIdentityNotFound) {
		return domain.ClientApplication{}, false, ErrApplicantNotRegistered
	}
	if err != nil {
		return domain.ClientApplication{}, false, fmt.Errorf("resolve applicant principal: %w", err)
	}
	if applicant.ID != req.ApplicantPrincipalID || applicant.Status != "ACTIVE" || applicant.ActorType != "human" || applicant.ID == operator.PrincipalID {
		return domain.ClientApplication{}, false, ErrApplicantNotRegistered
	}
	hash := ""
	if key != "" {
		hash, err = requestHash(raw)
		if err != nil {
			return domain.ClientApplication{}, false, err
		}
	}
	app, replayed, err := s.Repo.CreateClientApplication(ctx, applicant.ID, key, hash, operator.Audit,
		func(id, reference string) (domain.ClientApplication, repository.ApplicationChange, error) {
			at := s.now()
			created := domain.ClientApplication{
				ID: id, Reference: reference, Status: domain.ApplicationDraft, Channel: req.Channel,
				Version: 1, ApplicantPrincipalID: applicant.ID, OpenedByStaffPrincipalID: operator.PrincipalID,
				OrganisationProfile: json.RawMessage(`{}`), Requirements: json.RawMessage(`{}`),
				RequestedMarkets: []domain.RequestedMarket{}, Evidence: []domain.ApplicationEvidence{},
				InformationRequests: []domain.InformationRequest{}, CreatedAt: at, UpdatedAt: at,
			}
			d.apply(&created, at)
			if err := checkApplication(created); err != nil {
				return created, repository.ApplicationChange{}, err
			}
			return created, repository.ApplicationChange{
				AuditAction: "client_application.staff_created",
				AuditPayload: map[string]any{
					"application_channel": created.Channel, "reference": reference,
					"opened_by": operator.PrincipalID, "applicant_principal_id": applicant.ID, "reason": req.Reason,
				},
				EventType: events.ClientApplicationCreated,
				EventData: map[string]any{
					"client_application_id": id, "application_channel": created.Channel,
					"created_at": events.Timestamp(at),
				},
			}, nil
		})
	if errors.Is(err, repository.ErrApplicationIdempotencyConflict) {
		return app, false, ErrIdempotencyConflict
	}
	return app, replayed, err
}
