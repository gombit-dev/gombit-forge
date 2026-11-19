// Thin client for the control plane's cookie-gated API.
//
// Every request carries the session cookie (credentials: "include") and speaks
// the D10 envelope: success bodies are { data: ... } and errors are
// { error: { code, message, fields? } }. The control plane sets an HttpOnly
// session cookie on login, so the browser attaches it automatically and the SPA
// never handles a token itself (DESIGN.md D5).
//
// CSRF: the control plane runs Gombit's cookie auth, whose CSRF middleware
// refuses every POST/PUT/PATCH/DELETE (login included) that does not echo the
// JS-readable gombit_csrf cookie as an X-CSRF-Token header (double-submit; see
// gombit's docs/auth-cookie.md). The token is Gombit's: GET /auth/csrf mints
// it, and this client only reads the cookie and echoes it (D12). The session
// cookies are HttpOnly; the access cookie is short-lived, so a 401 is retried
// once after POST /auth/refresh rotates the session.

export const API_PREFIX = "/api/v1";

const CSRF_COOKIE = "gombit_csrf";
const CSRF_HEADER = "X-CSRF-Token";
const UNSAFE_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** readCookie returns the value of the named cookie, if the page can see it. */
function readCookie(name: string): string | undefined {
  for (const part of document.cookie.split(";")) {
    const [key, ...rest] = part.trim().split("=");
    if (key === name) {
      return decodeURIComponent(rest.join("="));
    }
  }
  return undefined;
}

/**
 * csrfToken returns the token to echo: the gombit_csrf cookie, bootstrapped
 * through GET /auth/csrf when it is missing, or always when fresh is set (the
 * control plane refused the one we sent). The cookie is the authoritative
 * value; the response body mirrors it for a page that cannot read cookies.
 */
async function csrfToken(fresh = false): Promise<string | undefined> {
  if (!fresh) {
    const existing = readCookie(CSRF_COOKIE);
    if (existing) {
      return existing;
    }
  }
  const resp = await fetch(API_PREFIX + "/auth/csrf", {
    credentials: "include",
    headers: { Accept: "application/json" },
  });
  let mirrored: string | undefined;
  if (resp.ok) {
    try {
      mirrored = ((await resp.json()) as DataEnvelope<{ csrf_token?: string }>).data?.csrf_token;
    } catch {
      // No JSON body: rely on the cookie.
    }
  }
  return readCookie(CSRF_COOKIE) ?? mirrored;
}

// refreshing is the in-flight POST /auth/refresh, shared so concurrent 401s
// rotate the session once.
let refreshing: Promise<boolean> | undefined;

/** refreshSession rotates the session cookies; true when it succeeded. */
function refreshSession(): Promise<boolean> {
  refreshing ??= (async () => {
    try {
      const resp = await send("/auth/refresh", { method: "POST" });
      return resp.ok;
    } finally {
      refreshing = undefined;
    }
  })();
  return refreshing;
}

/**
 * send issues one call with the session cookie and, for a state-changing
 * method, the CSRF header. A CSRF refusal (403 from the middleware) is retried
 * once with a freshly bootstrapped token, since the cookie may have expired or
 * been minted under a rotated secret.
 */
async function send(path: string, init?: RequestInit): Promise<Response> {
  const unsafe = UNSAFE_METHODS.has((init?.method ?? "GET").toUpperCase());
  const attempt = async (fresh: boolean) => {
    const token = unsafe ? await csrfToken(fresh) : undefined;
    return fetch(API_PREFIX + path, {
      ...init,
      credentials: "include",
      headers: {
        Accept: "application/json",
        ...(init?.body ? { "Content-Type": "application/json" } : {}),
        ...(token ? { [CSRF_HEADER]: token } : {}),
        ...init?.headers,
      },
    });
  };
  const resp = await attempt(false);
  if (unsafe && resp.status === 403 && (await isCSRFRefusal(resp))) {
    return attempt(true);
  }
  return resp;
}

/** isCSRFRefusal reports whether a 403 is the CSRF middleware's. */
async function isCSRFRefusal(resp: Response): Promise<boolean> {
  try {
    const body = (await resp.clone().json()) as ErrorEnvelope;
    return /csrf/i.test(body.error?.message ?? "");
  } catch {
    return false;
  }
}

/** The error a failed API call throws, carrying the envelope's fields. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields?: Record<string, string[]>;

  constructor(status: number, code: string, message: string, fields?: Record<string, string[]>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string; fields?: Record<string, string[]> };
}

interface DataEnvelope<T> {
  data: T;
}

/**
 * request issues a call to the control plane and unwraps the { data } envelope.
 * A non-2xx response is thrown as an ApiError built from the { error } envelope.
 */
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let resp = await send(path, init);
  // An expired access cookie is a 401 with a still-valid refresh cookie behind
  // it: rotate once and retry. Not for the auth endpoints themselves, where a
  // 401 is the answer (wrong credentials, no session).
  if (resp.status === 401 && !path.startsWith("/auth/") && (await refreshSession())) {
    resp = await send(path, init);
  }

  if (!resp.ok) {
    let code = "error";
    let message = resp.statusText || "request failed";
    let fields: Record<string, string[]> | undefined;
    try {
      const body = (await resp.json()) as ErrorEnvelope;
      if (body.error) {
        code = body.error.code ?? code;
        message = body.error.message ?? message;
        fields = body.error.fields;
      }
    } catch {
      // Non-JSON error body: keep the status text.
    }
    throw new ApiError(resp.status, code, message, fields);
  }

  if (resp.status === 204) {
    return undefined as T;
  }
  const body = (await resp.json()) as DataEnvelope<T>;
  return body.data;
}

// describeError turns an error into a message for the editor. For a validation
// failure it surfaces the field-keyed diagnostics (path: message) so the problem
// points at the offending entity rather than a generic "invalid" — the rest fall
// back to the envelope message.
export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.fields) {
      const parts = Object.entries(err.fields).flatMap(([key, msgs]) => msgs.map((m) => `${key}: ${m}`));
      if (parts.length > 0) {
        return parts.join("; ");
      }
    }
    return err.message;
  }
  return "Something went wrong";
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "PATCH", body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "PUT", body: body === undefined ? undefined : JSON.stringify(body) }),
  delete: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};
