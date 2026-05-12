import { defineConfig } from "tsdown";

// platform: "node" defaults ESM output to .mjs; the package.json exports point at
// dist/index.js so we override to keep stable filenames across builds.
export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm", "cjs"],
  dts: true,
  sourcemap: true,
  target: "node22",
  clean: true,
  platform: "node",
  nodeProtocol: true,
  outExtensions: ({ format }) => ({ js: format === "es" ? ".js" : ".cjs" }),
});
