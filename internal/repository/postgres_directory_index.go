package repository

// CX-05D: only CP's already evaluated EXACT organisation.view grant IDs may
// enter this query. The database never expands corporate/group membership,
// acts on a user-supplied principal, or returns unrestricted global rows.

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
)

type DirectoryIndexFilter struct {
    Query, Sort, Verification, Status, After string
    Limit int
}
type DirectoryIndexEntry struct {
    OrganisationID, DisplayName, Status, VerificationState, SourceAuthority, Jurisdiction string
}
type DirectoryIndexResult struct {
    Items []DirectoryIndexEntry
    TotalVisible int
    NextAfter string
}
var ErrDirectoryIndexCursor = errors.New("directory cursor no longer belongs to this authorised result")
var ErrDirectoryIndexBudget = errors.New("directory query exceeds bounded resource budget")

// DirectoryIndexReader is a bounded, authority-filtered SQL read model.
// Each request is a single REPEATABLE READ transaction; cross-request
// consistency is conditional on the authoritative data remaining unchanged,
// so a removed/moved cursor is rejected rather than silently traversed.
type DirectoryIndexReader interface {
    SearchAuthorisedOrganisations(context.Context, []string, DirectoryIndexFilter) (DirectoryIndexResult, error)
}

var _ DirectoryIndexReader = (*PostgresRepository)(nil)

// SearchAuthorisedOrganisations restricts every row and cursor anchor to the
// currently EFFECTIVE grant IDs supplied by the Control Plane, irrespective
// of source filter or sort. Tenant identifiers and emails are never inputs.
func (r *PostgresRepository) SearchAuthorisedOrganisations(ctx context.Context, ids []string, f DirectoryIndexFilter) (DirectoryIndexResult, error) {
    out := DirectoryIndexResult{Items: []DirectoryIndexEntry{}}
    if r == nil || r.pool == nil { return out, errors.New("directory index is unavailable") }
    if len(ids) > 20000 || f.Limit < 1 || f.Limit > 50 ||
        (f.Sort != "name-asc" && f.Sort != "name-desc") {
        return out, ErrDirectoryIndexBudget
    }
    if len(ids) == 0 {
        if f.After != "" {return out, ErrDirectoryIndexCursor}
        return out, nil
    }
    tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
    if err != nil {return out, err}
    defer tx.Rollback(ctx)
    // The array is made exclusively from evaluated CP grants, not from URL
    // query values. The result also excludes non-organisations and missing
    // profiles; no policy here infers access from parenthood.
    const base = ` FROM registry.organisation_profile o
        WHERE o.canonical_entity_id = ANY($1::uuid[])
          AND ($2 = '' OR position(lower($2) in lower(o.display_name)) > 0)
          AND ($3 = '' OR o.verification_state = $3)
          AND ($4 = '' OR o.status = $4)`
    if err := tx.QueryRow(ctx, `SELECT COUNT(*)`+base,
        ids, f.Query, f.Verification, f.Status).Scan(&out.TotalVisible); err != nil {
        return out, fmt.Errorf("count authorised organisation candidates: %w", err)
    }
    var cursorName string
    if f.After != "" {
        err := tx.QueryRow(ctx, `SELECT lower(o.display_name)`+base+` AND o.canonical_entity_id=$5::uuid`,
            ids, f.Query, f.Verification, f.Status, f.After).Scan(&cursorName)
        if errors.Is(err, pgx.ErrNoRows) {return out, ErrDirectoryIndexCursor}
        if err != nil {return out, err}
    }
    comparator, order := ">", "ASC"
    if f.Sort == "name-desc" {comparator,order="<","DESC"}
    // Tie breaker UUID is ALWAYS ascending within identical names.
    // Parentheses guarantee cursor iteration follows that total ordering.
    cursorWhere := ` AND ($5::uuid IS NULL OR
          (lower(o.display_name) `+comparator+` $6 OR
           (lower(o.display_name) = $6 AND o.canonical_entity_id > $5::uuid)))`
    var after any
    if f.After != "" { after = f.After }
    rows, err := tx.Query(ctx, `SELECT o.canonical_entity_id::text, o.display_name, o.status,
        o.verification_state, o.source_authority, COALESCE(o.jurisdiction,'')`+
        base+cursorWhere+` ORDER BY lower(o.display_name) `+order+`,
        o.canonical_entity_id ASC LIMIT $7`,
        ids, f.Query, f.Verification, f.Status, after, cursorName, f.Limit+1)
    if err != nil {return out, fmt.Errorf("page authorised organisations: %w",err)}
    for rows.Next() {
        var item DirectoryIndexEntry
        if err := rows.Scan(&item.OrganisationID,&item.DisplayName,&item.Status,
            &item.VerificationState,&item.SourceAuthority,&item.Jurisdiction); err != nil {
            rows.Close(); return out,err
        }
        out.Items=append(out.Items,item)
    }
    if err := rows.Err(); err!=nil {rows.Close();return out,err}
    rows.Close()
    if len(out.Items)>f.Limit {
        out.Items=out.Items[:f.Limit]
        out.NextAfter=out.Items[len(out.Items)-1].OrganisationID
    }
    if err:=tx.Commit(ctx);err!=nil{return DirectoryIndexResult{},err}
    return out,nil
}

// The SQL uses the server-indexed organisation name and UUID tie-breaker;
// the query budget is enforced before the database receives the ID array.
var _ = time.Time{}
