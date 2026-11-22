import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, call, contract, describeError } from "./client";

describe("describeError", () => {
  it("surfaces field diagnostics keyed to the offending path", () => {
    const err = new ApiError(422, "validation_error", "candidate spec is invalid", {
      "$.resources[0].fields[1].code_name": ["already used by field fld_1"],
    });
    const msg = describeError(err);
    expect(msg).toContain("$.resources[0].fields[1].code_name");
    expect(msg).toContain("already used");
  });

  it("falls back to the envelope message when there are no fields", () => {
    expect(describeError(new ApiError(409, "conflict", "candidate is ABI-breaking"))).toBe("candidate is ABI-breaking");
  });

  it("handles a non-ApiError", () => {
    expect(describeError(new TypeError("boom"))).toBe("Something went wrong");
  });
});

// --- CSRF and session refresh against a scripted control plane ---
//
// Through the typed client, the path every call takes (contract over send()).

interface Call {
  url: string;
  method: string;
  csrf: string | null;
}

function clearCookies() {
  for (const part of document.cookie.split(";")) {
    const name = part.split("=")[0].trim();
    if (name) {
      document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/`;
    }
  }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const csrfRefusal = () => json(403, { error: { code: "authorization", message: "csrf token missing or invalid" } });

/**
 * controlPlane mimics Gombit's cookie auth: GET /auth/csrf sets gombit_csrf to
 * the next minted token, an unsafe request without a matching X-CSRF-Token is
 * refused, and route answers everything else.
 */
function controlPlane(route: (url: string, method: string) => Response, tokens = ["tok-1", "tok-2"]) {
  const calls: Call[] = [];
  let minted = 0;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    const headers = new Headers(init?.headers);
    calls.push({ url, method, csrf: headers.get("X-CSRF-Token") });
    if (url.endsWith("/auth/csrf")) {
      const token = tokens[Math.min(minted++, tokens.length - 1)];
      document.cookie = `gombit_csrf=${token}; path=/`;
      return json(200, { data: { csrf_token: token } });
    }
    const cookie = /(?:^|; )gombit_csrf=([^;]*)/.exec(document.cookie)?.[1];
    if (method !== "GET" && (!cookie || headers.get("X-CSRF-Token") !== cookie)) {
      return csrfRefusal();
    }
    return route(url, method);
  }) as typeof fetch;
  return calls;
}

describe("CSRF double-submit", () => {
  beforeEach(clearCookies);
  afterEach(() => vi.restoreAllMocks());

  it("bootstraps the token and echoes it on login", async () => {
    const calls = controlPlane(() => json(200, { data: { id: 1 } }));
    await call(contract.POST("/api/v1/auth/login", { body: { email: "a@example.com", password: "pw" } }));
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual(["GET /api/v1/auth/csrf", "POST /api/v1/auth/login"]);
    expect(calls[1].csrf).toBe("tok-1");
  });

  it("reuses the cookie instead of bootstrapping again", async () => {
    document.cookie = "gombit_csrf=existing; path=/";
    const calls = controlPlane(() => json(200, { data: {} }));
    await call(contract.POST("/api/v1/organizations", { body: { name: "Acme", slug: "acme" } }));
    expect(calls).toHaveLength(1);
    expect(calls[0].csrf).toBe("existing");
  });

  it("sends no token on a safe request", async () => {
    const calls = controlPlane(() => json(200, { data: [] }));
    await call(contract.GET("/api/v1/organizations"));
    expect(calls).toEqual([{ url: "/api/v1/organizations", method: "GET", csrf: null }]);
  });

  it("re-bootstraps once when the control plane refuses the token", async () => {
    document.cookie = "gombit_csrf=stale; path=/";
    let refused = false;
    const calls = controlPlane(() => json(200, { data: {} }));
    const inner = globalThis.fetch;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      // The stale cookie is invalid server-side even though it matches.
      if (!refused && String(input).endsWith("/organizations/1/projects")) {
        refused = true;
        calls.push({ url: String(input), method: "POST", csrf: new Headers(init?.headers).get("X-CSRF-Token") });
        return csrfRefusal();
      }
      return inner(input, init);
    }) as typeof fetch;
    await call(
      contract.POST("/api/v1/organizations/{orgID}/projects", {
        params: { path: { orgID: "1" } },
        body: { name: "P", slug: "p" },
      }),
    );
    expect(calls.map((c) => `${c.method} ${c.url} ${c.csrf}`)).toEqual([
      "POST /api/v1/organizations/1/projects stale",
      "GET /api/v1/auth/csrf null",
      "POST /api/v1/organizations/1/projects tok-1",
    ]);
  });

  it("does not retry a 403 that is not the CSRF middleware's", async () => {
    const calls = controlPlane(() => json(403, { error: { code: "forbidden", message: "not a member" } }));
    await expect(
      call(
        contract.POST("/api/v1/organizations/{orgID}/projects", {
          params: { path: { orgID: "1" } },
          body: { name: "P", slug: "p" },
        }),
      ),
    ).rejects.toMatchObject({ status: 403 });
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
  });
});

describe("session refresh", () => {
  beforeEach(clearCookies);
  afterEach(() => vi.restoreAllMocks());

  it("rotates the session once on a 401 and retries", async () => {
    let expired = true;
    const calls = controlPlane((url) => {
      if (url.endsWith("/auth/refresh")) {
        expired = false;
        return new Response(null, { status: 204 });
      }
      return expired ? json(401, { error: { code: "unauthorized", message: "expired" } }) : json(200, { data: { id: 7 } });
    });
    await expect(call(contract.GET("/api/v1/me"))).resolves.toEqual({ id: 7 });
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
      "GET /api/v1/me",
      "GET /api/v1/auth/csrf",
      "POST /api/v1/auth/refresh",
      "GET /api/v1/me",
    ]);
  });

  it("does not refresh for an auth endpoint's own 401", async () => {
    const calls = controlPlane(() => json(401, { error: { code: "unauthorized", message: "invalid credentials" } }));
    await expect(
      call(contract.POST("/api/v1/auth/login", { body: { email: "a", password: "b" } })),
    ).rejects.toMatchObject({ status: 401 });
    expect(calls.some((c) => c.url.endsWith("/auth/refresh"))).toBe(false);
  });

  it("surfaces the 401 when the refresh fails", async () => {
    controlPlane(() => json(401, { error: { code: "unauthorized", message: "no session" } }));
    await expect(call(contract.GET("/api/v1/me"))).rejects.toMatchObject({ status: 401 });
  });
});
