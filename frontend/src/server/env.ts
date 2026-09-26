import "server-only";

import { z } from "zod";

// Server-only configuration, validated once at startup (instrumentation.ts)
// and never sent to the browser. Nothing here may use the NEXT_PUBLIC_
// prefix: Next.js inlines those into client bundles (prompt section 97).

const environments = ["development", "integration", "staging", "production"] as const;

const schema = z
  .object({
    CONSOLE_ENVIRONMENT: z.enum(environments),
    // The Go Control Plane API the BFF calls server-side. The browser never
    // calls it directly (ADR-BCP-019 section 43).
    CP_API_BASE_URL: z.url(),
  })
  .superRefine((env, ctx) => {
    const url = new URL(env.CP_API_BASE_URL);
    const local = url.hostname === "localhost" || url.hostname === "127.0.0.1";
    if (url.protocol !== "https:" && !(env.CONSOLE_ENVIRONMENT === "development" && local)) {
      ctx.addIssue({
        code: "custom",
        path: ["CP_API_BASE_URL"],
        message: "must use https outside local development",
      });
    }
  });

export type ServerEnv = z.infer<typeof schema>;

/** Parses and validates server configuration; throws listing every problem. */
export function parseServerEnv(source: Record<string, string | undefined>): ServerEnv {
  const leaked = Object.keys(source).filter((name) => name.startsWith("NEXT_PUBLIC_") && /SECRET|TOKEN|KEY|PASSWORD/i.test(name));
  const result = schema.safeParse(source);
  const problems = [
    ...leaked.map((name) => `${name}: secrets must never use the NEXT_PUBLIC_ prefix`),
    ...(result.success ? [] : result.error.issues.map((issue) => `${issue.path.join(".")}: ${issue.message}`)),
  ];
  if (problems.length > 0 || !result.success) {
    throw new Error(`invalid CP Console configuration: ${problems.join("; ")}`);
  }
  return result.data;
}

let cached: ServerEnv | undefined;

/** The validated server configuration of this process. */
export function serverEnv(): ServerEnv {
  cached ??= parseServerEnv(process.env);
  return cached;
}
