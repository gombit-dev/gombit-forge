#!/usr/bin/env bash
# Runs the editor's Playwright suite against a real control plane (`make
# web-e2e`): a throwaway Postgres in Docker, the committed migrations applied
# with atlas (as internal/dbtest does), the server binary and the SPA built,
# then `playwright test`, whose config starts the server (in an empty
# directory, so it reads no .env) and `vite preview`. The container and the
# temporary directory are removed on every exit path.
#
# Needs Docker, atlas, Go, node, and Playwright's Chromium
# (`npx playwright install chromium` once, in controlplane/web).
set -euo pipefail

web="$(cd "$(dirname "$0")/.." && pwd)"
migrations="$web/../database/migrations"
name="forge-web-e2e-pg-$$"
port="${FORGE_E2E_PG_PORT:-15432}"
dsn="postgres://postgres:forge@127.0.0.1:${port}/controlplane?sslmode=disable"

work="$(mktemp -d)"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

docker run -d --rm --name "$name" \
  -e POSTGRES_PASSWORD=forge -e POSTGRES_DB=controlplane \
  -p "127.0.0.1:${port}:5432" postgres:15 >/dev/null

# Ready over TCP, not the init-time unix socket (pg_isready -h 127.0.0.1).
for _ in $(seq 1 60); do
  if docker exec "$name" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$name" pg_isready -h 127.0.0.1 -U postgres >/dev/null

# env -i, like the server's own start (playwright.config.ts): atlas's Postgres
# driver reads the libpq PG* variables too, and the shell's must not change
# the migration.
env -i PATH="$PATH" HOME="$HOME" atlas migrate apply --dir "file://${migrations}" --url "$dsn"

# Built ahead, so the config's readiness timeout measures the boot, not the
# compiler. -buildvcs=false: the binary is a fixture, and VCS stamping fails in
# some checkouts (git worktrees under a sandbox).
(cd "$web/.." && go build -buildvcs=false -o "$work/forge-server" ./cmd/server)
mkdir "$work/run"

cd "$web"
npm run build
FORGE_E2E_DATABASE_DSN="$dsn" FORGE_E2E_SERVER_BIN="$work/forge-server" FORGE_E2E_SERVER_DIR="$work/run" \
  npx playwright test "$@"
