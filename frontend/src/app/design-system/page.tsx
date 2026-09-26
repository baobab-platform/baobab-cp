import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { serverEnv } from "@/server/env";

import { DesignSystemShowcase } from "./showcase";

export const metadata: Metadata = { title: "Design system · Baobab Control Plane Console" };

// Reads configuration at request time.
export const dynamic = "force-dynamic";

/**
 * The component catalogue for review and, later, accessibility end-to-end
 * tests. Its data is illustrative, so it is not served in production.
 */
export default function DesignSystemPage() {
  if (serverEnv().CONSOLE_ENVIRONMENT === "production") {
    notFound();
  }
  return <DesignSystemShowcase />;
}
