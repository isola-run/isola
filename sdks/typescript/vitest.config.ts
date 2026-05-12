import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["tests/**/*.test.ts"],
    exclude: ["tests/e2e/**/*.test.ts", "node_modules/**", "dist/**"],
    testTimeout: 10_000,
    coverage: {
      provider: "v8",
      reporter: ["text", "html", "lcov"],
      include: ["src/**/*.ts"],
      exclude: ["src/index.ts", "src/version.ts"],
      thresholds: {
        lines: 97,
        functions: 97,
        branches: 95,
        statements: 97,
      },
    },
  },
});
