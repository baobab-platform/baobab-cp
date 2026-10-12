import type { Metadata } from "next";
import { Landing } from "@/features/public-site/Landing";

/** Public information entry; privileged work stays under /workspace. */
export const metadata: Metadata = {
  title: "Baobab Platform | One platform. Every business possibility.",
  description: "Explore Baobab's governed enterprise architecture, solutions, progressive onboarding and developer resources.",
};
export default function Home() { return <Landing />; }
