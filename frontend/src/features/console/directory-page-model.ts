import { z } from "zod";

const uuid = z.string().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
const query = z.object({
  q: z.string().trim().max(80).optional(),
  sort: z.enum(["name-asc", "name-desc"]).optional(),
  verification: z.enum(["UNVERIFIED", "PENDING_REVIEW", "VERIFIED", "CONFLICTED", "REJECTED", "EXPIRED"]).optional(),
  status: z.string().regex(/^[A-Z_]{1,40}$/).optional(),
  limit: z.coerce.number().int().min(1).max(50).optional(),
  after: uuid.optional(),
}).strict();

export type DirectoryPageQuery = z.infer<typeof query>;

export function parseDirectoryPageRequest(raw: Record<string, string | string[] | undefined>): DirectoryPageQuery | null {
  if (Object.values(raw).some((item) => Array.isArray(item))) return null;
  const nonempty = Object.fromEntries(Object.entries(raw).filter(([, value]) => value !== undefined && value !== ""));
  const parsed = query.safeParse(nonempty);
  return parsed.success ? parsed.data : null;
}

const item = z.object({
  organisation_id: uuid,
  display_name: z.string().min(1),
  status: z.string().min(1),
  verification_state: z.string().min(1),
  source_authority: z.string().min(1),
  jurisdiction: z.string().optional(),
});
const result = z.object({
  evaluated_at: z.string(),
  page_size: z.number().int().positive().max(50),
  sort: z.enum(["name-asc", "name-desc"]),
  total_visible: z.number().int().nonnegative().max(20000),
  items: z.array(item).max(50),
  next_after: uuid.optional(),
});

export type DirectoryPageResult = z.infer<typeof result>;
export function parseDirectoryPageResult(input: unknown): DirectoryPageResult | null {
  const parsed = result.safeParse(input);
  if (!parsed.success || parsed.data.items.length > parsed.data.page_size ||
      parsed.data.items.length > parsed.data.total_visible) return null;
  return parsed.data;
}

export function directoryPageHref(page: DirectoryPageQuery): string {
  const params = new URLSearchParams();
  if (page.q) params.set("q", page.q);
  if (page.sort) params.set("sort", page.sort);
  if (page.verification) params.set("verification", page.verification);
  if (page.status) params.set("status", page.status);
  if (page.limit) params.set("limit", String(page.limit));
  if (page.after) params.set("after", page.after);
  const suffix = params.toString();
  return "/workspace/organisations" + (suffix ? "?" + suffix : "");
}
