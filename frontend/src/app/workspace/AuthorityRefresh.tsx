"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

/**
 * Refresh the server-owned authority/context snapshot during long-lived
 * sessions. This is a presentation freshness mechanism, never an auth check.
 * Destinations still re-authorise independently on the server.
 */
export function AuthorityRefresh() {
  const router = useRouter();

  useEffect(() => {
    const refresh = () => router.refresh();
    const refreshWhenVisible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refreshWhenVisible);
    const interval = window.setInterval(refresh, 60_000);

    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refreshWhenVisible);
      window.clearInterval(interval);
    };
  }, [router]);

  return null;
}
