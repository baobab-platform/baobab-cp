import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";
import Link from "next/link";
import type { ReactNode } from "react";

import { SESSION_COOKIE, createAuthStore, currentSession, readAuthSettings } from "@/server/auth/runtime";

import styles from "./workspace.module.css";

export const dynamic = "force-dynamic";

export default async function WorkspaceLayout({ children }: { children: ReactNode }) {
  let settings;
  try {
    settings = readAuthSettings();
  } catch {
    notFound();
  }
  const cookieStore = await cookies();
  let valid = false;
  try {
    await currentSession(createAuthStore(settings), cookieStore.get(SESSION_COOKIE)?.value, settings);
    valid = true;
  } catch {
    valid = false;
  }
  if (!valid) redirect("/api/auth/login");
  return (
    <main id="main" className="page">
      <header className={styles.top}>
        <Link href="/" className={styles.brand}>Baobab <span>Control Plane</span></Link>
        <nav aria-label="Onboarding workspace">
          <Link href="/workspace">Overview</Link>
          <Link href="/workspace/apply">My application</Link>
          <Link href="/workspace/review">Independent review</Link>
          <Link href="/workspace/operations">Operations</Link>
        </nav>
      </header>
      <div className={styles.content}>{children}</div>
    </main>
  );
}
