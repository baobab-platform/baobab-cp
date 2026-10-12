import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";
import Link from "next/link";
import type { ReactNode } from "react";
import { ArrowUpRight, LockKeyhole, Network } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { SESSION_COOKIE, createAuthStore, currentSession, readAuthSettings } from "@/server/auth/runtime";
import { WorkspaceNavigation } from "./WorkspaceNavigation";
import { CommandPalette } from "./CommandPalette";
import { WorkspaceAuthorityProvider } from "@/features/console/authority-context";
import { unavailableAuthority, summariseEffectiveAuthority } from "@/server/bff/authority";
import { invokeProgressive } from "@/server/bff/progressive";

export const dynamic = "force-dynamic";

/** Security checks remain server-side and happen before any protected shell is rendered. */
export default async function WorkspaceLayout({ children }: { children: ReactNode }) {
  let settings;
  try {
    settings = readAuthSettings();
  } catch {
    notFound();
  }
  const cookieStore = await cookies();
  const session = await currentSession(
    createAuthStore(settings), cookieStore.get(SESSION_COOKIE)?.value, settings,
  ).catch(() => null);
  if (!session) redirect("/api/auth/login");
  // GET /v1/admin/effective-authority requires the current caller's own grant.
  // This is a hint only; CP independently authorises every subsequent action.
  const authority = await invokeProgressive(
    { action: "effective-authority" }, session.accessToken,
  ).then((response) => summariseEffectiveAuthority(response.data, session.principalId))
    .catch(() => unavailableAuthority);

  return (
    <main id="main" className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border bg-card">
        <div className="mx-auto flex min-h-20 max-w-7xl flex-wrap items-center justify-between gap-4 px-5 py-3 lg:px-10">
          <Link href="/workspace" className="flex items-center gap-3 no-underline" aria-label="Baobab Control Plane Console overview">
            <span className="grid size-10 place-items-center rounded-lg bg-brand-forest text-brand-gold">
              <Network aria-hidden="true" className="size-5" />
            </span>
            <span className="flex flex-col leading-tight">
              <strong className="text-base font-bold tracking-tight text-brand-ink">Baobab</strong>
              <span className="text-xs text-muted-foreground">Control Plane Console</span>
            </span>
          </Link>
          <div className="flex flex-wrap items-center gap-3">
            <Badge variant="neutral"><LockKeyhole aria-hidden="true" className="size-3" />Protected workspace</Badge>
            <CommandPalette authority={authority} />
            <Link href="/" className="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-primary underline-offset-4 hover:underline">
              Platform website <ArrowUpRight aria-hidden="true" className="size-4" />
            </Link>
          </div>
        </div>
      </header>
      <WorkspaceAuthorityProvider value={authority}>
        <div className="mx-auto grid max-w-7xl gap-6 px-4 py-6 sm:px-6 md:grid-cols-[15rem_minmax(0,1fr)] lg:gap-8 lg:px-10 lg:py-10">
          <WorkspaceNavigation />
          <div className="min-w-0">{children}</div>
        </div>
      </WorkspaceAuthorityProvider>
    </main>
  );
}
