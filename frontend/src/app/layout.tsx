import type { Metadata } from "next";
import type { ReactNode } from "react";

import { SkipLink } from "@/components";
import "@/styles/globals.css";

/** Noindex until legal notices, domain, contact, availability and launch approval are real. */
export const metadata: Metadata = {
  title: { default: "Baobab Platform", template: "%s" },
  description: "Baobab Platform: a connected enterprise foundation for independently operated businesses.",
  robots: { index: false, follow: false },
};
export default function RootLayout({ children }: { children: ReactNode }) {
  return <html lang="en"><body><SkipLink />{children}</body></html>;
}
