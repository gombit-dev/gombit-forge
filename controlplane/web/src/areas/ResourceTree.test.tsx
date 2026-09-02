import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ResourceTree } from "./ResourceTree";
import type { SpecResource } from "../api/projects";

type Reply = { status: number; body?: unknown };
function mockApi(routes: (url: string, init?: RequestInit) => Reply) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const { status, body } = routes(url, init);
    return { ok: status >= 200 && status < 300, status, statusText: "", json: async () => body } as Response;
  });
  globalThis.fetch = fn as unknown as typeof fetch;
  return fn;
}

const CUSTOMER: SpecResource = {
  id: "res_1",
  label: "Customer",
  label_plural: "Customers",
  code_name: "Customer",
  storage_name: "customers",
};

afterEach(() => vi.restoreAllMocks());

describe("ResourceTree", () => {
  it("adds a resource from a label and reloads", async () => {
    const onChanged = vi.fn();
    const fetchMock = mockApi((url, init) => {
      if (url.endsWith("/projects/3/resources") && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        expect(body).toEqual({ label: "Order", label_plural: "Orders" });
        return { status: 201, body: { data: { id: 9, spec_hash: "x" } } };
      }
      return { status: 404 };
    });

    render(<ResourceTree projectID={3} resources={[]} onChanged={onChanged} />);
    const user = userEvent.setup();

    const form = screen.getByRole("form", { name: "Add resource" });
    await user.type(within(form).getByLabelText("New resource label"), "Order");
    await user.type(within(form).getByLabelText("New resource plural"), "Orders");
    await user.click(within(form).getByRole("button", { name: "Add resource" }));

    expect(fetchMock).toHaveBeenCalled();
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("renames a resource (labels only)", async () => {
    const onChanged = vi.fn();
    mockApi((url, init) => {
      if (url.endsWith("/projects/3/resources/res_1") && init?.method === "PATCH") {
        const body = JSON.parse(String(init.body));
        expect(body).toEqual({ label: "Client", label_plural: "Clients" });
        return { status: 201, body: { data: { id: 10, spec_hash: "y", abi_class: "neutral" } } };
      }
      return { status: 404 };
    });

    render(<ResourceTree projectID={3} resources={[CUSTOMER]} onChanged={onChanged} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Rename" }));
    const form = screen.getByRole("form", { name: "Rename Customer" });
    const label = within(form).getByLabelText("Label");
    await user.clear(label);
    await user.type(label, "Client");
    const plural = within(form).getByLabelText("Plural label");
    await user.clear(plural);
    await user.type(plural, "Clients");
    await user.click(within(form).getByRole("button", { name: "Save" }));

    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("confirms then deletes a resource", async () => {
    const onChanged = vi.fn();
    mockApi((url, init) => {
      if (url.endsWith("/projects/3/resources/res_1") && init?.method === "DELETE") {
        return { status: 200, body: { data: { committed: true, revision_id: 11, had_extension: false } } };
      }
      return { status: 404 };
    });

    render(<ResourceTree projectID={3} resources={[CUSTOMER]} onChanged={onChanged} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete" }));

    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("surfaces dependency blockers when a delete is blocked", async () => {
    const onChanged = vi.fn();
    mockApi((url, init) => {
      if (url.endsWith("/projects/3/resources/res_1") && init?.method === "DELETE") {
        return {
          status: 200,
          body: {
            data: {
              committed: false,
              had_extension: false,
              blockers: [{ kind: "relationship", message: "relationship Invoice.Customer still references Customer" }],
            },
          },
        };
      }
      return { status: 404 };
    });

    render(<ResourceTree projectID={3} resources={[CUSTOMER]} onChanged={onChanged} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete" }));

    // The block is surfaced with the concrete reference; nothing was reloaded.
    expect(await screen.findByText(/Invoice.Customer still references Customer/)).toBeInTheDocument();
    expect(onChanged).not.toHaveBeenCalled();
  });

  it("shows an error when an edit fails", async () => {
    mockApi((url, init) => {
      if (url.endsWith("/projects/3/resources") && init?.method === "POST") {
        return { status: 422, body: { error: { code: "validation_error", message: "candidate spec is invalid" } } };
      }
      return { status: 404 };
    });

    render(<ResourceTree projectID={3} resources={[]} onChanged={vi.fn()} />);
    const user = userEvent.setup();
    const form = screen.getByRole("form", { name: "Add resource" });
    await user.type(within(form).getByLabelText("New resource label"), "Bad");
    await user.click(within(form).getByRole("button", { name: "Add resource" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/invalid/i);
  });
});
