import { defineConfig, devices } from "@playwright/test";

// The editor end to end, in a real browser against a real control plane: the
// Go server (go run ./cmd/server) on a Postgres with the committed migrations
// applied, and the built SPA behind `vite preview`, which proxies /api to the
// server the way the dev server does, so the session cookie is same-origin.
//
// `make web-e2e` (from the repo root) starts that Postgres, applies the
// migrations and sets FORGE_E2E_DATABASE_DSN; run it rather than this directly.
const dsn = process.env.FORGE_E2E_DATABASE_DSN;
if (!dsn) {
  throw new Error("FORGE_E2E_DATABASE_DSN is not set: run the suite with `make web-e2e`");
}
const apiPort = process.env.FORGE_E2E_API_PORT ?? "18080";
const webPort = process.env.FORGE_E2E_WEB_PORT ?? "14173";

export default defineConfig({
  testDir: "e2e",
  testMatch: /.*\.e2e\.ts$/, // not *.test.ts / *.spec.ts, which Vitest would pick up
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://127.0.0.1:${webPort}`,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "go run ./cmd/server",
      cwd: "..",
      url: `http://127.0.0.1:${apiPort}/readyz`,
      timeout: 180_000,
      stdout: "pipe",
      env: {
        ...(process.env as Record<string, string>),
        GOMBIT_ENV: "development",
        GOMBIT_HTTP_ADDR: `127.0.0.1:${apiPort}`,
        GOMBIT_DATABASE_DRIVER: "postgres",
        GOMBIT_DATABASE_DSN: dsn,
        GOMBIT_AUTH_MODE: "cookie",
        GOMBIT_JWT_SECRET: "forge-editor-e2e-secret-not-for-production-0001",
        // Plain HTTP on loopback, so the cookies can't be Secure.
        GOMBIT_COOKIE_SECURE: "false",
        GOMBIT_COOKIE_SAMESITE: "lax",
      },
    },
    {
      command: `npx vite preview --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}`,
      timeout: 60_000,
      env: {
        ...(process.env as Record<string, string>),
        FORGE_CONTROL_PLANE: `http://127.0.0.1:${apiPort}`,
      },
    },
  ],
});
