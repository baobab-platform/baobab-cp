package domain

// CX-05E: bounded, source-bound and independently approved Console export
// and bulk metadata intents. These are policy/domain guards, NOT public HTTP
// operations. Publication requires durable audit and idempotent transaction
// ports, current CP grants and protected staging acceptance.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ConsoleExport        = "REDACTED_CSV_EXPORT"
	ConsoleBulk          = "BULK_METADATA_PROPOSAL"
	maxConsoleExportRows = 1000
	maxConsoleBulkRows   = 50
)

var consoleUUID = regexp.MustCompile(`^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var consoleHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ConsoleDatasetIntent binds every target, purpose, recipient, digest, version
// and TTL. No browser-supplied id can override the current CP authority.
type ConsoleDatasetIntent struct {
	Kind, MakerPrincipalID, Purpose, RecipientReference, IdempotencyKey, Action string
	SourceFingerprint, ChangeFingerprint                                        string
	TargetIDs                                                                   []string
	ValidUntil                                                                  time.Time
}
type ConsoleDatasetApproval struct {
	PlanDigest, CheckerPrincipalID, EvidenceReference string
	ApprovedAt                                        time.Time
}
type ConsoleDatasetRecord struct {
	OrganisationID, DisplayName, Status, VerificationState, SourceAuthority, Jurisdiction string
	Revision                                                                              int64
}
type ConsoleMetadataChange struct {
	OrganisationID  string
	ExpectedVersion int64
	InternalLabel   string
}

// DatasetSourceFingerprint fingerprints precisely the redacted source fields,
// not issuer controlled snapshot labels or unverified source-wide queries.
func DatasetSourceFingerprint(input []ConsoleDatasetRecord) (string, error) {
	if len(input) == 0 || len(input) > maxConsoleExportRows {
		return "", errors.New("invalid export source bound")
	}
	rows := append([]ConsoleDatasetRecord(nil), input...)
	slices.SortFunc(rows, func(a, b ConsoleDatasetRecord) int { return strings.Compare(a.OrganisationID, b.OrganisationID) })
	h := sha256.New()
	for i, r := range rows {
		if !consoleUUID.MatchString(r.OrganisationID) || len(r.DisplayName) == 0 || len(r.DisplayName) > 500 ||
			len(r.SourceAuthority) == 0 || len(r.SourceAuthority) > 200 || len(r.Status) > 60 ||
			len(r.VerificationState) > 60 || len(r.Jurisdiction) > 80 || r.Revision < 0 {
			return "", errors.New("invalid export record")
		}
		if i > 0 && rows[i-1].OrganisationID == r.OrganisationID {
			return "", errors.New("duplicate source record")
		}
		for _, v := range []string{r.OrganisationID, r.DisplayName, r.Status, r.VerificationState, r.SourceAuthority, r.Jurisdiction} {
			if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
				return "", errors.New("invalid export field")
			}
			fmt.Fprintf(h, "%d:%s", len(v), v)
		}
		fmt.Fprintf(h, "revision:%d;", r.Revision)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func BulkChangeFingerprint(input []ConsoleMetadataChange) (string, error) {
	if len(input) == 0 || len(input) > maxConsoleBulkRows {
		return "", errors.New("bulk count exceeds governed bound")
	}
	changes := append([]ConsoleMetadataChange(nil), input...)
	slices.SortFunc(changes, func(a, b ConsoleMetadataChange) int { return strings.Compare(a.OrganisationID, b.OrganisationID) })
	h := sha256.New()
	for i, c := range changes {
		if !consoleUUID.MatchString(c.OrganisationID) || c.ExpectedVersion < 1 || len(c.InternalLabel) > 120 ||
			strings.TrimSpace(c.InternalLabel) == "" || !utf8.ValidString(c.InternalLabel) || strings.ContainsAny(c.InternalLabel, "\x00\r\n") {
			return "", errors.New("invalid versioned metadata change")
		}
		if i > 0 && changes[i-1].OrganisationID == c.OrganisationID {
			return "", errors.New("duplicate bulk target")
		}
		fmt.Fprintf(h, "%s:%d:%d:%s\n", c.OrganisationID, c.ExpectedVersion, len(c.InternalLabel), c.InternalLabel)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (r ConsoleDatasetIntent) CanonicalDigest(now time.Time) (string, error) {
	if r.Kind != ConsoleExport && r.Kind != ConsoleBulk {
		return "", errors.New("invalid dataset operation")
	}
	if r.MakerPrincipalID == "" || strings.TrimSpace(r.Purpose) == "" || len(r.Purpose) > 250 ||
		len(r.RecipientReference) == 0 || len(r.RecipientReference) > 128 ||
		len(r.IdempotencyKey) < 16 || len(r.IdempotencyKey) > 128 ||
		!consoleHash.MatchString(r.SourceFingerprint) ||
		!r.ValidUntil.After(now) || r.ValidUntil.After(now.Add(24*time.Hour)) {
		return "", errors.New("invalid operation provenance, digest or expiry")
	}
	// Delimited canonical encoding must not accept delimiter bytes in any field.
	for _, v := range []string{r.Kind, r.MakerPrincipalID, r.Purpose, r.RecipientReference, r.IdempotencyKey, r.Action} {
		if strings.ContainsRune(v, 0) {
			return "", errors.New("NUL is forbidden in plan fields")
		}
	}
	cap := maxConsoleBulkRows
	if r.Kind == ConsoleExport {
		cap = maxConsoleExportRows
		if r.Action != "" || r.ChangeFingerprint != "" {
			return "", errors.New("export may not contain a mutation")
		}
	} else if r.Action != "organisation.metadata.update" || !consoleHash.MatchString(r.ChangeFingerprint) {
		return "", errors.New("unsupported or unfingerprinted bulk action")
	}
	if len(r.TargetIDs) == 0 || len(r.TargetIDs) > cap {
		return "", errors.New("invalid target count")
	}
	ids := append([]string(nil), r.TargetIDs...)
	slices.Sort(ids)
	for i, id := range ids {
		if !consoleUUID.MatchString(id) || (i > 0 && ids[i-1] == id) {
			return "", errors.New("invalid or repeated target")
		}
	}
	payload := strings.Join([]string{r.Kind, r.MakerPrincipalID, r.Purpose, r.RecipientReference, r.IdempotencyKey, r.Action,
		r.SourceFingerprint, r.ChangeFingerprint, r.ValidUntil.UTC().Format(time.RFC3339Nano), strings.Join(ids, ",")}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), nil
}

// CanExecuteIntent checks the independent maker/checker, immutable plan,
// audit reservation and the CURRENT exact grants for EVERY affected resource.
// A visible ancestor or sibling never gives permission to export or mutate.
func CanExecuteIntent(intent ConsoleDatasetIntent, approval ConsoleDatasetApproval,
	now time.Time, allowed func(principalID, id, permission string) bool, auditReserved bool) error {
	digest, err := intent.CanonicalDigest(now)
	if err != nil {
		return err
	}
	if digest != approval.PlanDigest || approval.CheckerPrincipalID == "" ||
		approval.CheckerPrincipalID == intent.MakerPrincipalID || approval.ApprovedAt.IsZero() ||
		approval.ApprovedAt.After(now) || approval.EvidenceReference == "" || !auditReserved || allowed == nil {
		return errors.New("independent current approval and audited plan are required")
	}
	for _, id := range intent.TargetIDs {
		// Evaluate grants for the ACTUAL maker and checker as distinct principals.
		if !allowed(intent.MakerPrincipalID, id, "organisation.view") ||
			!allowed(approval.CheckerPrincipalID, id, "organisation.view") {
			return errors.New("missing independently scoped maker/checker organisation view")
		}
		if intent.Kind == ConsoleExport {
			if !allowed(intent.MakerPrincipalID, id, "organisation.export") ||
				!allowed(approval.CheckerPrincipalID, id, "organisation.export") {
				return errors.New("disclosure permission missing for maker or checker")
			}
		} else {
			if !allowed(intent.MakerPrincipalID, id, "organisation.bulk.propose") ||
				!allowed(intent.MakerPrincipalID, id, "organisation.manage") ||
				!allowed(approval.CheckerPrincipalID, id, "organisation.bulk.approve") {
				return errors.New("independent proposal, management or checker approval permission missing")
			}
		}
	}
	return nil
}
func safeConsoleCSVCell(s string) string {
	s = strings.Map(func(c rune) rune {
		if c == '\n' || c == '\r' || c == '\t' {
			return ' '
		}
		if c < 32 || c == 127 {
			return -1
		}
		return c
	}, s)
	leading := strings.TrimLeftFunc(strings.TrimPrefix(s, "\ufeff"), unicode.IsSpace)
	if leading != "" && strings.ContainsRune("=+-@", []rune(leading)[0]) {
		return "'" + s
	}
	return s
}

// BuildApprovedRedactedCSV may ONLY be invoked after the caller has reserved
// a durable audit and obtained a separate current checker approval.
func BuildApprovedRedactedCSV(intent ConsoleDatasetIntent, approval ConsoleDatasetApproval,
	rows []ConsoleDatasetRecord, now time.Time, allowed func(string, string, string) bool,
	auditReserved bool) ([]byte, error) {
	if intent.Kind != ConsoleExport {
		return nil, errors.New("not export")
	}
	if err := CanExecuteIntent(intent, approval, now, allowed, auditReserved); err != nil {
		return nil, err
	}
	current, err := DatasetSourceFingerprint(rows)
	if err != nil {
		return nil, err
	}
	if current != intent.SourceFingerprint || len(rows) != len(intent.TargetIDs) {
		return nil, errors.New("disclosure source changed")
	}
	approved := make(map[string]bool, len(intent.TargetIDs))
	for _, id := range intent.TargetIDs {
		approved[id] = true
	}
	sorted := append([]ConsoleDatasetRecord(nil), rows...)
	slices.SortFunc(sorted, func(a, b ConsoleDatasetRecord) int { return strings.Compare(a.OrganisationID, b.OrganisationID) })
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err = w.Write([]string{"organisation_id", "display_name", "status", "verification_state", "source_authority", "jurisdiction"}); err != nil {
		return nil, err
	}
	for _, r := range sorted {
		if !approved[r.OrganisationID] {
			return nil, errors.New("foreign source row")
		}
		if err = w.Write([]string{r.OrganisationID, safeConsoleCSVCell(r.DisplayName), safeConsoleCSVCell(r.Status),
			safeConsoleCSVCell(r.VerificationState), safeConsoleCSVCell(r.SourceAuthority), safeConsoleCSVCell(r.Jurisdiction)}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return nil, err
	}
	if buf.Len() > 2_000_000 {
		return nil, errors.New("export byte budget exceeded")
	}
	return buf.Bytes(), nil
}

type CommitConsoleMetadataBatch func(context.Context, string, []ConsoleMetadataChange) error

// ApplyApprovedMetadataBulk hands a fully approved batch to ONE atomic
// transaction adapter. It never loops over unauthorised public update APIs.
func ApplyApprovedMetadataBulk(ctx context.Context, intent ConsoleDatasetIntent,
	approval ConsoleDatasetApproval, source []ConsoleDatasetRecord, changes []ConsoleMetadataChange,
	now time.Time, allowed func(string, string, string) bool, auditReserved bool, commit CommitConsoleMetadataBatch) error {
	if intent.Kind != ConsoleBulk || commit == nil {
		return errors.New("bulk transaction adapter is unavailable")
	}
	if err := CanExecuteIntent(intent, approval, now, allowed, auditReserved); err != nil {
		return err
	}
	current, err := DatasetSourceFingerprint(source)
	if err != nil {
		return err
	}
	if current != intent.SourceFingerprint || len(changes) != len(intent.TargetIDs) || len(source) != len(intent.TargetIDs) {
		return errors.New("bulk source or target count changed")
	}
	approved := make(map[string]bool, len(intent.TargetIDs))
	for _, id := range intent.TargetIDs {
		approved[id] = true
	}
	currentSource := make(map[string]int64, len(source))
	for _, r := range source {
		if !approved[r.OrganisationID] || r.Revision < 1 {
			return errors.New("foreign or unversioned approved source")
		}
		currentSource[r.OrganisationID] = r.Revision
	}
	for _, c := range changes {
		if !approved[c.OrganisationID] || currentSource[c.OrganisationID] != c.ExpectedVersion {
			return errors.New("target is absent from current source or has a changed revision")
		}
	}
	hash, err := BulkChangeFingerprint(changes)
	if err != nil {
		return err
	}
	if hash != intent.ChangeFingerprint {
		return errors.New("bulk values changed after approval")
	}
	digest, err := intent.CanonicalDigest(now)
	if err != nil {
		return err
	}
	return commit(ctx, digest, changes)
}
