// PEO-03: read-only reviewer projection of applicant-owned v2 submissions.
// An applicant's DRAFT is never discoverable by staff; the reader is not an
// admission decider, verifier, tenant requester or legal-actor authority.
package postgres

import (
    "context"
    "errors"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/jackc/pgx/v5"
)

func (s *Store) GetProgressiveApplicationForReview(ctx context.Context, reviewerID, id string) (ProgressiveApplicantDraft,error) {
    var empty ProgressiveApplicantDraft
    if !domain.IsUUID(reviewerID) { return empty, ErrProgressiveApplicationNotFound }
    uuid,err:=domain.ParseResourceID(domain.ClientApplicationIDPrefix,id)
    if err!=nil { return empty,ErrProgressiveApplicationNotFound }
    app,err:=readProgressive(s.pool.QueryRow(ctx,`SELECT `+progressiveSelect+`
      FROM admission.client_application_v2
      WHERE client_application_id=$1::uuid AND status='SUBMITTED'
        AND applicant_principal_id<>$2::uuid`,uuid,reviewerID))
    if errors.Is(err,pgx.ErrNoRows) { return empty,ErrProgressiveApplicationNotFound }
    return app,err
}

// Review queue is deliberately bounded, status restricted, and has no
// magic "all applications" privilege. Staff never sees private drafts.
func (s *Store) ListProgressiveApplicationsForReview(ctx context.Context,reviewerID string,limit int) ([]ProgressiveApplicantDraft,error) {
    if !domain.IsUUID(reviewerID) { return nil,ErrProgressiveApplicationNotFound }
    if limit<1 || limit>100 { return nil,ErrProgressiveApplicationConflict }
    rows,err:=s.pool.Query(ctx,`SELECT `+progressiveSelect+`
      FROM admission.client_application_v2
      WHERE status='SUBMITTED' AND applicant_principal_id<>$1::uuid
      ORDER BY submitted_at DESC,client_application_id DESC
      LIMIT $2`,reviewerID,limit)
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=make([]ProgressiveApplicantDraft,0)
    for rows.Next() {
        app,err:=readProgressive(rows)
        if err!=nil{return nil,err}
        out=append(out,app)
    }
    if err=rows.Err();err!=nil{return nil,err}
    return out,nil
}
