// Session state for the editor, backed by the control plane's cookie auth.
//
// The provider probes GET /me on mount: 200 means an authenticated session
// (the cookie is valid), 401 means signed out. login() posts to /auth/login,
// which sets the HttpOnly cookies; logout() clears them. No session token ever
// touches JS (DESIGN.md D5) — the browser carries the cookies, and this only
// tracks who the current user is. The one value JS reads is Gombit's CSRF
// cookie, which is readable by design so the client can echo it (api/client).

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { ApiError, call, contract } from "../api/client";

export interface User {
  id: number;
  email: string;
}

type Status = "loading" | "authenticated" | "anonymous";

export interface Session {
  status: Status;
  user: User | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

async function fetchMe(): Promise<User | null> {
  try {
    return await call(contract.GET("/api/v1/me"));
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      return null;
    }
    throw err;
  }
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>("loading");
  const [user, setUser] = useState<User | null>(null);

  const refresh = useCallback(async () => {
    const me = await fetchMe();
    setUser(me);
    setStatus(me ? "authenticated" : "anonymous");
  }, []);

  useEffect(() => {
    // A network failure on the initial probe lands the app on the sign-in view
    // rather than a spinner that never resolves.
    void refresh().catch(() => {
      setUser(null);
      setStatus("anonymous");
    });
  }, [refresh]);

  const login = useCallback(async (email: string, password: string) => {
    await call(contract.POST("/api/v1/auth/login", { body: { email, password } }));
    await refresh();
  }, [refresh]);

  const logout = useCallback(async () => {
    // Only drop to anonymous once the server has actually invalidated the
    // session. The session cookie is HttpOnly and can die only server-side, so
    // optimistically clearing local state on a failed POST would leave a valid
    // cookie that the next /me probe signs straight back in — "I signed out but
    // I'm still signed in after a refresh". On failure the error propagates and
    // the session stays as it really is.
    await call(contract.POST("/api/v1/auth/logout"));
    setUser(null);
    setStatus("anonymous");
  }, []);

  const value = useMemo<Session>(
    () => ({ status, user, login, logout, refresh }),
    [status, user, login, logout, refresh],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): Session {
  const ctx = useContext(SessionContext);
  if (!ctx) {
    throw new Error("useSession must be used within a SessionProvider");
  }
  return ctx;
}
