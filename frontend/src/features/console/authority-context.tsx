"use client";
import { createContext, useContext, type ReactNode } from "react";
import type { AuthoritySummary } from "./navigation-model";

const defaultValue: AuthoritySummary = { state: "unavailable", permissions: [] };
const AuthorityContext = createContext<AuthoritySummary>(defaultValue);

/** Server-provided, caller-bound navigation hint. Never passes bearer tokens. */
export function WorkspaceAuthorityProvider({ value, children }: {
  value: AuthoritySummary; children: ReactNode;
}) {
  return <AuthorityContext.Provider value={value}>{children}</AuthorityContext.Provider>;
}
export function useWorkspaceAuthority() {
  return useContext(AuthorityContext);
}
