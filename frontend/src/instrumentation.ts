// Runs once when the server starts. The configuration check is Node-only,
// so it lives in its own module that the Edge runtime never loads.
export async function register(): Promise<void> {
  if (process.env.NEXT_RUNTIME === "nodejs") {
    const { validateConfigurationOrExit } = await import("./instrumentation-node");
    validateConfigurationOrExit();
  }
}
