import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { HealthPanel } from "./HealthPanel";
import { jsonResponse } from "../test/http";

function mockHealth(reply: { status: number; body?: unknown }) {
  globalThis.fetch = vi.fn(async () => jsonResponse(reply.status, reply.body)) as unknown as typeof fetch;
}

afterEach(() => vi.restoreAllMocks());

describe("HealthPanel", () => {
  it("shows the three states separately", async () => {
    mockHealth({
      status: 200,
      body: {
        data: {
          facets: [
            { name: "Spec", status: "ok", summary: "Valid" },
            { name: "Extension ABI", status: "ok", summary: "Compatible" },
            { name: "Runtime Build", status: "unknown", summary: "no toolchain available" },
          ],
        },
      },
    });
    render(<HealthPanel projectID={3} reloadKey={0} />);
    const panel = await screen.findByRole("complementary", { name: "Project health" });
    for (const name of ["Spec", "Extension ABI", "Runtime Build"]) {
      expect(panel).toHaveTextContent(name);
    }
    // The three stay distinct: build is unknown even though spec/ABI are ok.
    expect(panel).toHaveTextContent(/no toolchain available/i);
  });

  it("surfaces spec diagnostics keyed to the offending path", async () => {
    mockHealth({
      status: 200,
      body: {
        data: {
          facets: [
            { name: "Spec", status: "failed", summary: "Invalid (1 issue(s))" },
            { name: "Extension ABI", status: "unknown", summary: "Not evaluated" },
            { name: "Runtime Build", status: "unknown", summary: "no toolchain" },
          ],
          diagnostics: [{ path: "$.resources[0].fields[1].code_name", code: "duplicate_code_name", message: "already used", entity: "fld_1" }],
        },
      },
    });
    render(<HealthPanel projectID={3} reloadKey={0} />);
    const diags = await screen.findByRole("list", { name: "Spec diagnostics" });
    expect(diags).toHaveTextContent("$.resources[0].fields[1].code_name");
    expect(diags).toHaveTextContent("already used");
  });

  it("clears the previous project's health while the next project loads", async () => {
    const projectA = jsonResponse(200, {
      data: { facets: [{ name: "Project A health", status: "ok", summary: "A is healthy" }] },
    });
    const projectB = jsonResponse(200, {
      data: { facets: [{ name: "Project B health", status: "ok", summary: "B is healthy" }] },
    });

    let resolveProjectB!: (response: Response) => void;
    const projectBRequest = new Promise<Response>((resolve) => {
      resolveProjectB = resolve;
    });
    globalThis.fetch = vi
      .fn()
      .mockResolvedValueOnce(projectA)
      .mockImplementationOnce(() => projectBRequest) as unknown as typeof fetch;

    const { rerender } = render(<HealthPanel projectID={1} reloadKey={0} />);
    expect(await screen.findByText("Project A health")).toBeInTheDocument();

    rerender(<HealthPanel projectID={2} reloadKey={0} />);

    expect(screen.queryByText("Project A health")).not.toBeInTheDocument();
    expect(screen.queryByRole("complementary", { name: "Project health" })).not.toBeInTheDocument();

    await act(async () => {
      resolveProjectB(projectB);
    });
    expect(await screen.findByText("Project B health")).toBeInTheDocument();
  });

  // A same-project reload (reloadKey bump after an edit) must keep the last-good
  // facets visible while the refetch is in flight; only a project switch clears.
  it("keeps the current health visible while a same-project reload is in flight", async () => {
    const health = (summary: string) => jsonResponse(200, { data: { facets: [{ name: "Spec", status: "ok", summary }] } });

    let resolveReload!: (response: Response) => void;
    const reloadRequest = new Promise<Response>((resolve) => {
      resolveReload = resolve;
    });
    globalThis.fetch = vi
      .fn()
      .mockResolvedValueOnce(health("Valid before edit"))
      .mockImplementationOnce(() => reloadRequest) as unknown as typeof fetch;

    const { rerender } = render(<HealthPanel projectID={1} reloadKey={0} />);
    expect(await screen.findByText("Valid before edit")).toBeInTheDocument();

    rerender(<HealthPanel projectID={1} reloadKey={1} />);

    // The refetch is pending, but the panel still shows the previous facets.
    expect(screen.getByRole("complementary", { name: "Project health" })).toHaveTextContent("Valid before edit");

    await act(async () => {
      resolveReload(health("Valid after edit"));
    });
    expect(await screen.findByText("Valid after edit")).toBeInTheDocument();
    expect(screen.queryByText("Valid before edit")).not.toBeInTheDocument();
  });

  // A transient health-fetch failure must not latch: once a later load (a bumped
  // reloadKey after an edit) succeeds, the panel shows live health again, never
  // the stale error. Guards against the panel going permanently dark on one blip.
  it("recovers after a transient fetch error when a later load succeeds", async () => {
    // A function: a Response's body can be read once, and the panel may load
    // more than once after the error.
    const ok = () =>
      jsonResponse(200, {
        data: {
          facets: [
            { name: "Spec", status: "ok", summary: "Valid" },
            { name: "Extension ABI", status: "ok", summary: "Compatible" },
            { name: "Runtime Build", status: "unknown", summary: "no toolchain" },
          ],
        },
      });
    let call = 0;
    globalThis.fetch = vi.fn(async () => {
      call += 1;
      if (call === 1) return jsonResponse(500, {});
      return ok();
    }) as unknown as typeof fetch;

    const { rerender } = render(<HealthPanel projectID={3} reloadKey={0} />);
    // The first fetch fails: the error branch renders, no facets.
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    // A later edit bumps reloadKey; the second fetch succeeds.
    rerender(<HealthPanel projectID={3} reloadKey={1} />);
    const panel = await screen.findByRole("complementary", { name: "Project health" });
    expect(panel).toHaveTextContent("Spec");
    // The stale error is gone — the panel reflects live health again.
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  });
});
