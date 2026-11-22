import { expect, test, type APIRequestContext } from "@playwright/test";

// One editor session against a real control plane: the checks the unit tests
// can't make, because they stub fetch. Gombit's cookie session and CSRF
// double-submit, the generated client's paths and bodies, and the server's
// committed revisions all have to work together here.

type Seeded = {
  email: string;
  password: string;
  orgName: string;
  projectName: string;
  projectID: number;
  api: APIRequestContext;
};

test("sign in, add a resource and a date field, and sign out", async ({ page, playwright, baseURL }) => {
  const problems: string[] = [];
  page.on("pageerror", (err) => problems.push(`page error: ${err.message}`));
  page.on("console", (msg) => {
    // Chrome logs every 4xx/5xx load as a console error; the response
    // listener below judges those, so only other console errors count here.
    if (msg.type() === "error" && !msg.text().startsWith("Failed to load resource")) {
      problems.push(`console error: ${msg.text()}`);
    }
  });
  page.on("response", (resp) => {
    const path = new URL(resp.url()).pathname;
    if (!path.startsWith("/api/") || resp.status() < 400) return;
    // Signed out, the editor's session check (/me) and its one refresh attempt
    // answer 401: that is how it knows to show the sign-in form.
    const expected = resp.status() === 401 && (path === "/api/v1/me" || path === "/api/v1/auth/refresh");
    if (!expected) problems.push(`${resp.request().method()} ${path} → ${resp.status()}`);
  });

  const seeded = await seed(await playwright.request.newContext({ baseURL }));

  // Sign in through the editor's own form: its transport fetches the CSRF
  // cookie and echoes it on the login POST.
  await page.goto("/");
  const signIn = page.getByRole("form", { name: "Sign in" });
  await signIn.getByLabel("Email").fill(seeded.email);
  await signIn.getByLabel("Password").fill(seeded.password);
  await signIn.getByRole("button", { name: "Sign in" }).click();
  const areas = page.getByRole("navigation", { name: "Editor areas" });
  await expect(areas).toBeVisible();

  // The session is a cookie, so it survives a reload.
  await page.reload();
  await expect(areas).toBeVisible();

  await areas.getByRole("link", { name: "Data" }).click();
  await page.getByRole("combobox", { name: "Organization" }).selectOption({ label: seeded.orgName });
  await page.getByRole("combobox", { name: "Project" }).selectOption({ label: seeded.projectName });

  const addResource = page.getByRole("form", { name: "Add resource" });
  await addResource.getByLabel("New resource label").fill("Invoice");
  await addResource.getByRole("button", { name: "Add resource" }).click();
  await expect(page.getByRole("list", { name: "Resources" }).getByText("Invoice", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Fields of Invoice" }).click();
  const addField = page.getByRole("form", { name: "Add field" });
  await addField.getByLabel("Field label").fill("Issued on");
  await addField.getByLabel("Field type").selectOption("date");
  await addField.getByRole("button", { name: "Add field" }).click();
  await expect(page.getByRole("list", { name: "Fields" }).getByText("Issued on")).toBeVisible();

  // Each edit committed a revision: the head spec the server serves has both.
  const spec = await getJSON(seeded.api, `/api/v1/projects/${seeded.projectID}/spec`);
  const invoice = (spec.resources ?? []).find((r: { label?: string }) => r.label === "Invoice");
  expect(invoice, "the head revision has the Invoice resource").toBeTruthy();
  expect(
    (invoice.fields ?? []).find((f: { label?: string; type?: string }) => f.label === "Issued on" && f.type === "date"),
    "the Invoice resource has the date field",
  ).toBeTruthy();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("form", { name: "Sign in" })).toBeVisible();

  expect(problems, "no page or console errors").toEqual([]);
  await seeded.api.dispose();
});

// seed registers a user and creates an organization and a project through the
// API: the editor has no UI for either. It signs in with its own cookie jar,
// separate from the browser's.
async function seed(api: APIRequestContext): Promise<Seeded> {
  const run = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;
  const email = `editor-e2e-${run}@example.com`;
  const password = "editor-e2e-password-0001";

  await post(api, "/api/v1/auth/register", { email, password });
  await post(api, "/api/v1/auth/login", { email, password });
  const orgName = `E2E Org ${run}`;
  const org = await post(api, "/api/v1/organizations", { name: orgName, slug: `e2e-org-${run}` });
  const projectName = `E2E Project ${run}`;
  const project = await post(api, `/api/v1/organizations/${org.id}/projects`, {
    name: projectName,
    slug: `e2e-project-${run}`,
  });
  return { email, password, orgName, projectName, projectID: project.id, api };
}

// post sends a state-changing request the way the editor does: Gombit's CSRF
// double-submit echoes the gombit_csrf cookie as X-CSRF-Token.
async function post(api: APIRequestContext, path: string, body: unknown) {
  const csrf = await csrfToken(api);
  const resp = await api.post(path, { data: body, headers: { "X-CSRF-Token": csrf } });
  expect(resp.ok(), `POST ${path} → ${resp.status()} ${await resp.text()}`).toBeTruthy();
  return (await resp.json()).data;
}

async function getJSON(api: APIRequestContext, path: string) {
  const resp = await api.get(path);
  expect(resp.ok(), `GET ${path} → ${resp.status()} ${await resp.text()}`).toBeTruthy();
  return (await resp.json()).data;
}

async function csrfToken(api: APIRequestContext): Promise<string> {
  const { cookies } = await api.storageState();
  let cookie = cookies.find((c) => c.name === "gombit_csrf");
  if (!cookie) {
    const resp = await api.get("/api/v1/auth/csrf");
    expect(resp.ok(), `GET /api/v1/auth/csrf → ${resp.status()}`).toBeTruthy();
    cookie = (await api.storageState()).cookies.find((c) => c.name === "gombit_csrf");
  }
  if (!cookie) throw new Error("the control plane set no gombit_csrf cookie");
  return cookie.value;
}
