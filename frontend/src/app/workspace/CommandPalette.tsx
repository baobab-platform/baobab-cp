"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ArrowRight, Command, Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { AuthoritySummary } from "@/features/console/navigation-model";
import { visibleConsoleRoutes } from "@/features/console/navigation-model";

/** Navigation-only palette: no server resource search, grants or writes.
 * A native modal dialog supplies focus management and Escape-to-close.
 */
export function CommandPalette({ authority }: { authority: AuthoritySummary }) {
  const router = useRouter();
  const modal = useRef<HTMLDialogElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const routes = useMemo(() => visibleConsoleRoutes(authority), [authority]);
  const matches = useMemo(() => {
    const search = query.trim().toLocaleLowerCase();
    return routes.filter((route) => (route.label + " " + route.description).toLocaleLowerCase().includes(search));
  }, [query, routes]);

  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "k") {
        event.preventDefault();
        setOpen((current) => !current);
      }
    }
    document.addEventListener("keydown", shortcut);
    return () => document.removeEventListener("keydown", shortcut);
  }, []);
  useEffect(() => {
    if (open && !modal.current?.open) {
      modal.current?.showModal();
      input.current?.focus();
    } else if (!open && modal.current?.open) {
      modal.current.close();
    }
  }, [open]);

  function navigate(href: string) {
    setOpen(false);
    router.push(href);
  }
  function listKeys(event: React.KeyboardEvent<HTMLDivElement>) {
    if (!["ArrowUp", "ArrowDown"].includes(event.key)) return;
    const links = [...(list.current?.querySelectorAll<HTMLButtonElement>("button[data-command]") ?? [])];
    if (links.length === 0) return;
    event.preventDefault();
    const index = links.indexOf(document.activeElement as HTMLButtonElement);
    const next = event.key === "ArrowDown"
      ? (index + 1) % links.length
      : index < 0 ? links.length - 1 : (index - 1 + links.length) % links.length;
    links[next]?.focus();
  }
  return (
    <>
      <Button variant="outline" type="button" onClick={() => { setQuery(""); setOpen(true); }}
        aria-haspopup="dialog" aria-keyshortcuts="Control+K Meta+K" className="gap-3">
        <Search aria-hidden="true" /><span className="hidden sm:inline">Find a page</span>
        <kbd className="hidden rounded border border-border px-1.5 py-0.5 text-xs text-muted-foreground lg:inline">⌘ / Ctrl K</kbd>
      </Button>
      <dialog ref={modal} aria-labelledby="command-title"
        onClose={() => setOpen(false)} onCancel={() => setOpen(false)}
        className="w-[min(38rem,calc(100vw-2rem))] rounded-2xl border border-border bg-card p-0 text-foreground shadow-2xl backdrop:bg-brand-deep/70">
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <h2 id="command-title" className="flex items-center gap-2 text-base font-bold">
            <Command aria-hidden="true" className="size-5 text-primary" /> Find a Console page
          </h2>
          <Button variant="ghost" size="icon" type="button" aria-label="Close page search" onClick={() => setOpen(false)}>
            <X aria-hidden="true" />
          </Button>
        </div>
        <div onKeyDown={listKeys}>
          <label className="sr-only" htmlFor="console-command-search">Search available Console pages</label>
          <input ref={input} autoComplete="off" id="console-command-search" type="search"
            value={query} onChange={(event) => setQuery(event.target.value)}
            placeholder="Search your available pages…"
            className="w-full border-0 bg-card px-5 py-4 text-base text-foreground outline-offset-[-3px] placeholder:text-muted-foreground" />
          <div ref={list} className="max-h-72 overflow-y-auto border-t border-border px-3 py-3">
            {matches.length > 0 ? matches.map((route) => (
              <button key={route.href} type="button" data-command
                onClick={() => navigate(route.href)}
                className="flex w-full items-center justify-between gap-3 rounded-lg px-4 py-3 text-left hover:bg-muted focus-visible:bg-muted focus-visible:outline-2 focus-visible:outline-focus">
                <span className="flex flex-col gap-1"><strong className="text-sm">{route.label}</strong>
                  <span className="text-xs text-muted-foreground">{route.description}</span></span>
                <ArrowRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
              </button>
            )) : <p className="px-4 py-6 text-sm text-muted-foreground" role="status">No matching authorised pages.</p>}
          </div>
        </div>
        <p className="border-t border-border px-5 py-3 text-xs text-muted-foreground">
          Navigation only. Every Control Plane action is authorised again when invoked.
        </p>
      </dialog>
    </>
  );
}
