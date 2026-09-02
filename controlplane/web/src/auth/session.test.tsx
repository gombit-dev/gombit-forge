import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SessionProvider, useSession } from "./session";

function mockMe(reply: { status: number; body?: unknown }) {
  globalThis.fetch = vi.fn(async () => ({
    ok: reply.status >= 200 && reply.status < 300,
    status: reply.status,
    statusText: "",
    json: async () => reply.body,
  })) as unknown as typeof fetch;
}

// probe renders the session status and user, so a test can read the provider's
// resolved state from the DOM.
function Probe() {
  const { status, user } = useSession();
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="user">{user?.email ?? "none"}</span>
    </div>
  );
}

afterEach(() => vi.restoreAllMocks());

describe("SessionProvider", () => {
  it("resolves to authenticated when /me returns a user", async () => {
    mockMe({ status: 200, body: { data: { id: 5, email: "grace@example.test" } } });
    render(
      <SessionProvider>
        <Probe />
      </SessionProvider>,
    );
    expect(await screen.findByText("authenticated")).toBeInTheDocument();
    expect(screen.getByTestId("user")).toHaveTextContent("grace@example.test");
  });

  it("resolves to anonymous when /me is 401", async () => {
    mockMe({ status: 401, body: { error: { code: "unauthorized" } } });
    render(
      <SessionProvider>
        <Probe />
      </SessionProvider>,
    );
    expect(await screen.findByText("anonymous")).toBeInTheDocument();
    expect(screen.getByTestId("user")).toHaveTextContent("none");
  });

  it("falls back to anonymous when the probe network-fails", async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new TypeError("network down");
    }) as unknown as typeof fetch;
    render(
      <SessionProvider>
        <Probe />
      </SessionProvider>,
    );
    expect(await screen.findByText("anonymous")).toBeInTheDocument();
  });
});
