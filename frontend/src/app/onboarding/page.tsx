import { notFound } from "next/navigation";

import { ProgressiveShowcase } from "@/features/onboarding/ProgressiveShowcase";
import { serverEnv } from "@/server/env";

// Public informational preview is not an administrative portal. Production
// stays unavailable until the FE-03 confidential OIDC+BFF session is proven.
export default function OnboardingPreviewPage() {
  if (serverEnv().CONSOLE_ENVIRONMENT === "production") {
    notFound();
  }
  return <ProgressiveShowcase />;
}
