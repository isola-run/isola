import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["tests/e2e/**/*.test.ts"],
    exclude: ["node_modules/**", "dist/**"],
    testTimeout: 120_000,
    hookTimeout: 180_000,
    pool: "threads",
    fileParallelism: false,
  },
});
