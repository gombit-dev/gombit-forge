import { defineConfig, devices } from "@playwright/test";

// The editor end to end, in a real browser against a real control plane: the
// server binary on a Postgres with the committed migrations applied, and the
// built SPA behind `vite preview`, which proxies /api to the server the way the
// dev server does, so the session cookie is same-origin.
//
// `make web-e2e` (from the repo root) starts that Postgres, applies the
// migrations, builds the server and sets the variables below; run it rather
// than this directly.
function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set: run the suite with \`make web-e2e\``);
  return value;
}

const dsn = required("FORGE_E2E_DATABASE_DSN");
const serverBin = required("FORGE_E2E_SERVER_BIN");
const serverDir = required("FORGE_E2E_SERVER_DIR");
const apiPort = process.env.FORGE_E2E_API_PORT ?? "18080";
const webPort = process.env.FORGE_E2E_WEB_PORT ?? "14173";

// The server's whole configuration. Nothing else reaches it:
// - It runs in an empty directory (serverDir), so gombit's config.Load finds no
//   .env to fill in what this leaves out.
// - Playwright always passes its own environment to a web server, under this
//   object, so the command unsets every inherited variable the control plane
//   reads (GOMBIT_*, FORGE_*, GITHUB_*). A developer's SMTP relay, Redis cache,
//   token TTLs or Cloud credentials can't change what the test runs against.
const serverEnv: Record<string, string> = {
  GOMBIT_ENV: "development",
  GOMBIT_HTTP_ADDR: `127.0.0.1:${apiPort}`,
  GOMBIT_DATABASE_DRIVER: "postgres",
  GOMBIT_DATABASE_DSN: dsn,
  GOMBIT_AUTH_MODE: "cookie",
  GOMBIT_JWT_SECRET: "forge-editor-e2e-secret-not-for-production-0001",
  // Plain HTTP on loopback, so the cookies can't be Secure.
  GOMBIT_COOKIE_SECURE: "false",
  GOMBIT_COOKIE_SAMESITE: "lax",
};
const inherited = Object.keys(process.env).filter(
  (name) => /^(GOMBIT|FORGE|GITHUB)_[A-Z0-9_]*$/.test(name) && !(name in serverEnv),
);
const unset = inherited.map((name) => `-u ${name}`).join(" ");

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
      // A built binary, so the timeout measures the boot, not the compiler.
      command: `env ${unset} "${serverBin}"`,
      cwd: serverDir,
      url: `http://127.0.0.1:${apiPort}/readyz`,
      timeout: 60_000,
      stdout: "pipe",
      env: serverEnv,
    },
    {
      command: `npx vite preview --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}`,
      timeout: 60_000,
      env: { FORGE_CONTROL_PLANE: `http://127.0.0.1:${apiPort}` },
    },
  ],
});
