import type { Metadata } from "next";
import type { ReactNode } from "react";

import { SkipLink } from "@/components";

import "@/styles/globals.css";

export const metadata: Metadata = {
  title: "Baobab Control Plane Console",
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>
        <SkipLink />
        {children}
      </body>
    </html>
  );
}
