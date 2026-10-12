import Link from "next/link";
import { ArrowUpRight, Building2, Fingerprint, FolderSearch, ShieldCheck } from "lucide-react";
import { z } from "zod";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { invokeProgressive } from "@/server/bff/progressive";
import { authenticatedConsoleActor } from "@/server/console/scoped-context";

export const dynamic = "force-dynamic";

const directorySchema = z.object({
  evaluated_at: z.string(),
  items: z.array(z.object({
    organisation_id: z.string().uuid(),
    display_name: z.string().min(1),
    status: z.string().min(1),
    verification_state: z.string().min(1),
    source_authority: z.string().min(1),
    jurisdiction: z.string().optional(),
  })).max(100),
});
function parseDirectory(input: unknown) {
  const parsed = directorySchema.safeParse(input);
  return parsed.success ? parsed.data : null;
}

/** These are sourced from the server-validated current caller, not a global
 * search index, a guessed corporate tree or locally stored choices.
 */
export default async function OrganisationsDirectory() {
  const actor = await authenticatedConsoleActor();
  const response = await invokeProgressive(
    { action: "organisation-directory" }, actor.accessToken,
  ).then((value) => value.data).catch(() => null);
  const directory = parseDirectory(response);
  return (
    <section aria-labelledby="directory-title" className="space-y-7">
      <div className="rounded-2xl bg-brand-forest p-7 text-white sm:p-10">
        <div className="mb-4 flex items-center gap-2 text-amber-200">
          <FolderSearch aria-hidden="true" className="size-6" />
          <span className="text-xs font-bold uppercase tracking-wider">Governed enterprise directory</span>
        </div>
        <h1 id="directory-title" className="text-3xl font-bold tracking-tight sm:text-4xl">
          My organisations
        </h1>
        <p className="mt-3 max-w-2xl text-sm leading-relaxed text-emerald-50">
          Authoritative organisation records available through your currently effective,
          exact-scoped Control Plane access. Related businesses are not automatically accessible.
        </p>
      </div>
      {!directory ? (
        <Card><CardHeader><CardTitle>Directory unavailable</CardTitle></CardHeader>
          <CardContent><p role="status" className="text-sm text-muted-foreground">
            The directory could not be verified or is not enabled for this environment.
            No organisation list has been inferred from your profile or browser state.
          </p></CardContent></Card>
      ) : directory.items.length === 0 ? (
        <Card><CardHeader><CardTitle>No directly accessible organisations</CardTitle>
          <CardDescription>Last authority evaluation: {directory.evaluated_at}</CardDescription>
        </CardHeader><CardContent><p className="text-sm text-muted-foreground">
          No exact organisation grants were resolved to current records. This does not mean
          other corporate relationships or applications are absent.
        </p></CardContent></Card>
      ) : (
        <>
          <div className="flex items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">{directory.items.length} authorised organisation records · Evaluated {directory.evaluated_at}</p>
            <ShieldCheck aria-label="Current caller-scoped view" className="size-5 text-primary" />
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            {directory.items.map((org) => (
              <Card key={org.organisation_id}>
                <CardHeader>
                  <Building2 aria-hidden="true" className="size-6 text-primary" />
                  <CardTitle>{org.display_name}</CardTitle>
                  <CardDescription className="break-all">{org.organisation_id}</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="neutral">{org.status}</Badge>
                    <Badge variant="neutral">{org.verification_state.replaceAll("_", " ")}</Badge>
                    {org.jurisdiction ? <Badge variant="neutral">{org.jurisdiction}</Badge> : null}
                  </div>
                  <p className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
                    <Fingerprint aria-hidden="true" className="size-4" />Source: {org.source_authority}
                  </p>
                  <Link href={"/workspace/organisations/" + encodeURIComponent(org.organisation_id)}
                    className="mt-5 inline-flex min-h-11 items-center gap-2 font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-focus">
                    Open Organisation 360° <ArrowUpRight aria-hidden="true" className="size-4" />
                  </Link>
                </CardContent>
              </Card>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
