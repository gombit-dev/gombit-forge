import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DataArea } from "./DataArea";

// mockApi answers each control-plane path from a table so DataArea drives the
// real client over a stubbed fetch.
function mockApi(routes: (url: string) => { status: number; body?: unknown }) {
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const { status, body } = routes(url);
    return { ok: status >= 200 && status < 300, status, statusText: "", json: async () => body } as Response;
  }) as unknown as typeof fetch;
}

afterEach(() => vi.restoreAllMocks());

const ORG = { id: 7, name: "Acme", slug: "acme" };
const PROJECT = { id: 3, organization_id: 7, name: "Acme CRM", slug: "acme-crm" };
const SPEC = {
  resources: [
    { id: "res_1", label: "Customer", label_plural: "Customers", code_name: "Customer", storage_name: "customers" },
  ],
};

function wire(specBody: unknown = { data: SPEC }) {
  mockApi((url) => {
    if (url.endsWith("/organizations")) return { status: 200, body: { data: [ORG] } };
    if (url.endsWith(`/organizations/${ORG.id}/projects`)) return { status: 200, body: { data: [PROJECT] } };
    if (url.endsWith(`/projects/${PROJECT.id}/spec`)) return { status: 200, body: specBody };
    return { status: 404 };
  });
}

describe("DataArea", () => {
  it("prompts to select a project before anything is chosen", async () => {
    wire();
    render(<DataArea />);
    // The org picker is populated from the API.
    expect(await screen.findByRole("option", { name: "Acme" })).toBeInTheDocument();
    expect(screen.getByText(/select a project to edit its data model/i)).toBeInTheDocument();
  });

  it("loads and lists the selected project's resources", async () => {
    wire();
    render(<DataArea />);
    const user = userEvent.setup();

    await screen.findByRole("option", { name: "Acme" });
    await user.selectOptions(screen.getByRole("combobox", { name: "Organization" }), "7");
    await user.selectOptions(await screen.findByRole("combobox", { name: "Project" }), "3");

    // The head spec's resources render, with their human-facing labels.
    const list = await screen.findByRole("list", { name: "Resources" });
    expect(list).toHaveTextContent("Customer");
    expect(list).toHaveTextContent("Customers");
  });

  it("shows an empty state for a project with no revisions", async () => {
    wire({ data: null });
    render(<DataArea />);
    const user = userEvent.setup();

    await screen.findByRole("option", { name: "Acme" });
    await user.selectOptions(screen.getByRole("combobox", { name: "Organization" }), "7");
    await user.selectOptions(await screen.findByRole("combobox", { name: "Project" }), "3");

    expect(await screen.findByText(/no resources yet/i)).toBeInTheDocument();
  });
});
