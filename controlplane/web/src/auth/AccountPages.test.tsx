import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BrowserRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "../App";
import { SessionProvider } from "./session";
import { jsonResponse } from "../test/http";

// The account pages are driven through the real App, session and client over a
// fetch stub that records every call, as App.test.tsx does.
type Reply = { status: number; body?: unknown };
type Call = { path: string; method: string; body: unknown };

function mockApi(routes: (path: string, init?: RequestInit) => Reply): Call[] {
  const calls: Call[] = [];
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const path = url.replace(/^.*\/api\/v1/, "");
    calls.push({ path, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    const { status, body } =
      path === "/auth/csrf" ? { status: 200, body: { data: { csrf_token: "csrf-1" } } } : routes(path, init);
    return jsonResponse(status, body);
  }) as unknown as typeof fetch;
  return calls;
}

const signedOut = (path: string): Reply | undefined =>
  path === "/me" ? { status: 401, body: { error: { code: "unauthorized" } } } : undefined;

function renderAt(url: string) {
  window.history.pushState({}, "", url);
  return render(
    <BrowserRouter>
      <SessionProvider>
        <App />
      </SessionProvider>
    </BrowserRouter>,
  );
}

const posts = (calls: Call[], path: string) => calls.filter((c) => c.method === "POST" && c.path === path);

afterEach(() => {
  vi.restoreAllMocks();
  window.history.pushState({}, "", "/");
});

describe("forgot password", () => {
  it("is linked from the sign-in view and requests a reset link", async () => {
    const calls = mockApi((path) => signedOut(path) ?? { status: 202, body: { data: { accepted: true } } });
    renderAt("/");
    await userEvent.click(await screen.findByRole("link", { name: /forgot password/i }));

    await userEvent.type(await screen.findByLabelText("Email"), "ada@example.test");
    await userEvent.click(screen.getByRole("button", { name: /send reset link/i }));

    expect(await screen.findByRole("status")).toHaveTextContent("If an account uses ada@example.test");
    expect(posts(calls, "/auth/password/forgot").map((c) => c.body)).toEqual([{ email: "ada@example.test" }]);
  });
});

describe("reset password", () => {
  it("posts the link's token with the new password and drops the token from the URL", async () => {
    const calls = mockApi((path) => signedOut(path) ?? { status: 200, body: { data: {} } });
    renderAt("/reset-password?token=reset-tok");

    await userEvent.type(await screen.findByLabelText("New password"), "a-new-long-passphrase");
    expect(window.location.search).toBe("");
    await userEvent.type(screen.getByLabelText("Repeat the new password"), "a-new-long-passphrase");
    await userEvent.click(screen.getByRole("button", { name: /set password/i }));

    expect(await screen.findByRole("status")).toHaveTextContent("every session is signed out");
    expect(posts(calls, "/auth/password/reset").map((c) => c.body)).toEqual([
      { token: "reset-tok", password: "a-new-long-passphrase" },
    ]);
  });

  it("refuses passwords that do not match without calling the API", async () => {
    const calls = mockApi((path) => signedOut(path) ?? { status: 200, body: { data: {} } });
    renderAt("/reset-password?token=reset-tok");

    await userEvent.type(await screen.findByLabelText("New password"), "one-passphrase");
    await userEvent.type(screen.getByLabelText("Repeat the new password"), "another-passphrase");
    await userEvent.click(screen.getByRole("button", { name: /set password/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent("do not match");
    expect(posts(calls, "/auth/password/reset")).toHaveLength(0);
  });

  it("shows the API's error and its field details", async () => {
    mockApi(
      (path) =>
        signedOut(path) ?? {
          status: 422,
          body: { error: { code: "validation", message: "invalid password", fields: { password: ["is too short"] } } },
        },
    );
    renderAt("/reset-password?token=reset-tok");

    await userEvent.type(await screen.findByLabelText("New password"), "short");
    await userEvent.type(screen.getByLabelText("Repeat the new password"), "short");
    await userEvent.click(screen.getByRole("button", { name: /set password/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent("invalid password: is too short");
  });

  it("explains a link with no token", async () => {
    mockApi((path) => signedOut(path) ?? { status: 404 });
    renderAt("/reset-password");
    expect(await screen.findByRole("alert")).toHaveTextContent("has no token");
    expect(screen.getByRole("link", { name: /request a new link/i })).toBeInTheDocument();
  });
});

describe("token confirmation links", () => {
  it("verifies an email only when the user confirms", async () => {
    const calls = mockApi((path) => signedOut(path) ?? { status: 200, body: { data: {} } });
    renderAt("/verify-email?token=verify-tok");

    const button = await screen.findByRole("button", { name: /verify email/i });
    expect(posts(calls, "/auth/verify-email")).toHaveLength(0);
    await userEvent.click(button);

    expect(await screen.findByRole("status")).toHaveTextContent("verified");
    expect(posts(calls, "/auth/verify-email").map((c) => c.body)).toEqual([{ token: "verify-tok" }]);
  });

  it("confirms an email change for a signed-in user and offers the editor", async () => {
    const calls = mockApi((path) => {
      if (path === "/me") return { status: 200, body: { data: { id: 1, email: "ada@example.test" } } };
      return { status: 200, body: { data: {} } };
    });
    renderAt("/confirm-email?token=change-tok");

    await userEvent.click(await screen.findByRole("button", { name: /confirm new email/i }));

    expect(await screen.findByRole("status")).toHaveTextContent("now your account's email");
    expect(posts(calls, "/auth/email/change/confirm").map((c) => c.body)).toEqual([{ token: "change-tok" }]);
    await waitFor(() => expect(screen.getByRole("link", { name: /continue to forge/i })).toBeInTheDocument());
  });

  it("shows why a spent or expired token failed", async () => {
    // Gombit's answer for a token it does not accept.
    mockApi(
      (path) =>
        signedOut(path) ?? {
          status: 422,
          body: {
            error: {
              code: "validation_error",
              message: "The request contains invalid fields.",
              fields: { token: ["invalid or expired token"] },
            },
          },
        },
    );
    renderAt("/verify-email?token=old-tok");

    await userEvent.click(await screen.findByRole("button", { name: /verify email/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /^The request contains invalid fields: invalid or expired token$/,
    );
  });
});
