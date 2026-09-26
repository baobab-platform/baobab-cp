// Runs once when the server starts. Invalid configuration stops the
// process: Next.js would otherwise log the error and keep serving.
export async function register(): Promise<void> {
  if (process.env.NEXT_RUNTIME !== "nodejs") {
    return;
  }
  const { serverEnv } = await import("@/server/env");
  try {
    serverEnv();
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  }
}
