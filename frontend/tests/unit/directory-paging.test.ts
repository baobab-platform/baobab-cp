import { describe, expect, it } from "vitest";

import { directoryPageHref, parseDirectoryPageRequest, parseDirectoryPageResult } from "@/features/console/directory-page-model";
import { resolveProgressiveCommand } from "@/server/bff/progressive";

const id = "0199d000-1111-7777-8000-111111111111";

describe("CX-05 caller-bound server directory paging", () => {
  it("uses an exact server-owned URL and permits only typed filters", () => {
    expect(resolveProgressiveCommand({ action: "organisation-directory-page",
      page: { q: "Coffee", sort: "name-desc", limit: 10, after: id, verification: "VERIFIED" } })).toEqual({
        method: "GET",
        path: "/v1/organisations/directory/page?q=Coffee&sort=name-desc&verification=VERIFIED&after="+id+"&limit=10",
    });
    for (const invalid of [
      { action: "organisation-directory-page", tenantId: "tn_other" },
      { action: "organisation-directory-page", id },
      { action: "organisation-directory-page", body: { actor: "admin" } },
      { action: "organisation-directory-page", page: { limit: 1000 } },
      { action: "organisation-directory-page", page: { after: "../../other" } },
      { action: "organisation-directory-page", page: { principal: "someone-else" } },
      { action: "organisation-directory-page", page: { sort: "authority-desc" } },
    ]) expect(() => resolveProgressiveCommand(invalid as never)).toThrow();
  });

  it("validates browser query text and refuses array/unknown query inputs", () => {
    expect(parseDirectoryPageRequest({ q: "Coffee", sort: "name-asc", limit: "25", verification: "" }))
      .toEqual({ q: "Coffee", sort: "name-asc", limit: 25 });
    expect(parseDirectoryPageRequest({ after: ["a", "b"] })).toBeNull();
    expect(parseDirectoryPageRequest({ principal_id: "someone" })).toBeNull();
    expect(parseDirectoryPageRequest({ limit: "999" })).toBeNull();
    expect(directoryPageHref({ q: "Coffee beans", limit: 10 }))
      .toBe("/workspace/organisations?q=Coffee+beans&limit=10");
  });

  it("rejects overfull, mismatched and untrusted result pages", () => {
    const item = {
      organisation_id: id, display_name: "Sample Co", status: "ACTIVE",
      verification_state: "PENDING_REVIEW", source_authority: "CP",
    };
    const valid = {
      evaluated_at: "2026-10-12T10:00:00Z", page_size: 10,
      sort: "name-asc", total_visible: 1, items: [item],
    };
    expect(parseDirectoryPageResult(valid)?.items).toHaveLength(1);
    expect(parseDirectoryPageResult({ ...valid, items: [item, item] })).toBeNull();
    expect(parseDirectoryPageResult({ ...valid, page_size: 1, items: [item,item] })).toBeNull();
    expect(parseDirectoryPageResult({ ...valid, items: [{...item,organisation_id: "../../other"}] })).toBeNull();
  });
});
