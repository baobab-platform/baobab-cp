import { serverEnv } from "@/server/env";

/** Invalid configuration stops the process: Next.js would otherwise log the error and keep serving. */
export function validateConfigurationOrExit(): void {
  try {
    serverEnv();
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  }
}
