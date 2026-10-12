"use client";

import { useEffect, useRef } from "react";

/** CX-05B: progressive enhancement of a native GET form.
 * The server owns search, permission filtering, cursor validation and URL
 * semantics. No browser API access, token, arbitrary principal or tenant
 * selector is introduced by this debounce.
 */
export function DirectorySearchInput({ defaultQuery }: { defaultQuery: string }) {
  const input = useRef<HTMLInputElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => () => {
    if (timer.current !== null) clearTimeout(timer.current);
  }, []);

  function scheduleSearch(value: string) {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
    if (value.trim() === defaultQuery.trim()) return;
    timer.current = setTimeout(() => {
      // A genuine GET form sends only the allowlisted inputs; notably the
      // previous cursor is not present, so a new query begins on page one.
      input.current?.form?.requestSubmit();
      timer.current = null;
    }, 450);
  }

  return (
    <>
      <input id="org-query" ref={input} name="q" type="search" defaultValue={defaultQuery}
        maxLength={80} aria-describedby="org-search-help" autoComplete="off"
        onChange={(event) => scheduleSearch(event.currentTarget.value)}
        placeholder="Search current access…"
        className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm text-foreground focus-visible:outline-2 focus-visible:outline-focus" />
      <p id="org-search-help" className="text-xs leading-relaxed text-muted-foreground">
        Updates 450 ms after typing stops. Press Enter to search immediately.
      </p>
    </>
  );
}
