import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypeScript from "eslint-config-next/typescript";

const config = [
  ...nextVitals,
  ...nextTypeScript,
  {
    ignores: [".next/**", "node_modules/**", "next-env.d.ts", "src/generated/**"],
  },
  {
    rules: {
      // The browser never reads server configuration: only src/server may
      // touch process.env (prompt section 97).
      "no-restricted-properties": [
        "error",
        { object: "process", property: "env", message: "Read configuration through src/server/env.ts." },
      ],
    },
  },
  {
    // instrumentation.ts reads NEXT_RUNTIME, which names the runtime rather
    // than configuring the Console.
    files: ["src/server/env.ts", "src/instrumentation.ts", "tests/**", "*.config.*"],
    rules: { "no-restricted-properties": "off" },
  },
];

export default config;
