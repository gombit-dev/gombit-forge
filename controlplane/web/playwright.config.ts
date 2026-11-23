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

// The server's whole environment. It starts under `env -i` with only these
// variables, written out on its command line (env -i also drops what Playwright
// passes a web server), so nothing in the shell reaches it: not the control
// plane's own GOMBIT_*/FORGE_*/GITHUB_* settings, and not a library's either,
// such as the libpq PG* variables its Postgres driver reads. It runs in an
// empty directory (serverDir), so gombit's config.Load finds no .env.
const serverEnv: Record<string, string> = {
  PATH: process.env.PATH ?? "/usr/bin:/bin",
  HOME: serverDir,
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
// shellQuote single-quotes v for the POSIX shell Playwright runs commands in.
const shellQuote = (v: string) => `'${v.replace(/'/g, `'\\''`)}'`;
const serverCommand = [
  "env -i",
  ...Object.entries(serverEnv).map(([name, value]) => `${name}=${shellQuote(value)}`),
  shellQuote(serverBin),
].join(" ");

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
      command: serverCommand,
      cwd: serverDir,
      url: `http://127.0.0.1:${apiPort}/readyz`,
      timeout: 60_000,
      stdout: "pipe",
    },
    {
      command: `npx vite preview --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}`,
      timeout: 60_000,
      env: { FORGE_CONTROL_PLANE: `http://127.0.0.1:${apiPort}` },
    },
  ],
});
