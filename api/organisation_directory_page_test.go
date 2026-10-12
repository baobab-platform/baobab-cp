package api

import (
	"net/url"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestCX05DirectoryQueryValidation(t *testing.T) {
	f, ok := parseDirectoryPageQuery(url.Values{"q": {"  Coffee "}, "sort": {"name-desc"}, "limit": {"10"}, "verification": {"VERIFIED"}})
	if !ok || f.Query != "Coffee" || f.Sort != "name-desc" || f.Limit != 10 || f.Verification != "VERIFIED" {
		t.Fatalf("valid query refused: %+v, %v", f, ok)
	}
	for name, v := range map[string]url.Values{
		"untrusted principal":  {"principal_id": {"prn_other"}},
		"wrong tenant":         {"tenant_id": {"tn_other"}},
		"duplicate filter":     {"q": {"A", "B"}},
		"invalid page size":    {"limit": {"1000"}},
		"arbitrary sort":       {"sort": {"source_authority"}},
		"invalid grant cursor": {"after": {"../../org"}},
		"oversized query":      {"q": {"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}},
		"invalid verification": {"verification": {"FORCED_VERIFIED"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseDirectoryPageQuery(v); ok {
				t.Fatalf("invalid query accepted: %v", v)
			}
		})
	}
}

func TestCX05DirectoryPageCurrentAuthorisationAndStableCursor(t *testing.T) {
	id1, id2, id3 := directoryOrg, directoryOther, "0199d000-1111-7777-8000-333333333333"
	entries := []organisationDirectoryEntry{
		{OrganisationID: id2, DisplayName: "Bravo Commerce", VerificationState: domain.VerificationPendingReview, Status: "ACTIVE"},
		{OrganisationID: id3, DisplayName: "Charlie Trade", VerificationState: domain.VerificationVerified, Status: "ACTIVE"},
		{OrganisationID: id1, DisplayName: "Alpha Logistics", VerificationState: domain.VerificationUnverified, Status: "PENDING"},
	}
	f := directoryPageFilter{Sort: "name-asc", Limit: 2}
	first, ok := pageDirectoryEntries(entries, f)
	if !ok || first.TotalVisible != 3 || len(first.Items) != 2 || first.Items[0].OrganisationID != id1 || first.NextAfter != id2 {
		t.Fatalf("wrong first page %+v", first)
	}
	f.After = first.NextAfter
	second, ok := pageDirectoryEntries(entries, f)
	if !ok || len(second.Items) != 1 || second.Items[0].OrganisationID != id3 || second.NextAfter != "" {
		t.Fatalf("wrong second page %+v", second)
	}
	f.After = "0199d000-1111-7777-8000-444444444444"
	if _, ok := pageDirectoryEntries(entries, f); ok {
		t.Fatal("foreign or revoked cursor must fail")
	}
	f = directoryPageFilter{Sort: "name-desc", Limit: 25, Query: "TRADE", Verification: "VERIFIED"}
	filtered, ok := pageDirectoryEntries(entries, f)
	if !ok || filtered.TotalVisible != 1 || filtered.Items[0].OrganisationID != id3 {
		t.Fatalf("wrong filtered result: %+v", filtered)
	}
	f = directoryPageFilter{Sort: "name-asc", Limit: 25, Status: "SUSPENDED"}
	empty, ok := pageDirectoryEntries(entries, f)
	if !ok || empty.TotalVisible != 0 || empty.Items == nil {
		t.Fatalf("should return a concrete empty page %+v", empty)
	}
}
