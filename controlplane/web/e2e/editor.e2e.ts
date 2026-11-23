import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

// One editor session against a real control plane: the checks the unit tests
// can't make, because they stub fetch. Gombit's cookie session and CSRF
// double-submit, the generated client's paths and bodies, and the server's
// committed revisions all have to work together here.

// Gombit's session cookies (auth.AccessCookieName, auth.RefreshCookieName).
const accessCookie = "gombit_access";
const refreshCookie = "gombit_refresh";

type Seeded = {
  email: string;
  password: string;
  orgName: string;
  projectName: string;
  projectID: number;
  api: APIRequestContext;
};

test("sign in, add a resource and a date field, and sign out", async ({ page, playwright, baseURL }) => {
  const watch = watchForProblems(page);
  await reportProblemsFirst(watch.problems, async () => {
    const seeded = await seed(await playwright.request.newContext({ baseURL }));

    // Sign in through the editor's own form: its transport fetches the CSRF
    // cookie and echoes it on the login POST.
    await page.goto("/");
    const signIn = page.getByRole("form", { name: "Sign in" });
    await signIn.getByLabel("Email").fill(seeded.email);
    await signIn.getByLabel("Password").fill(seeded.password);
    // The form renders only once the signed-out session check (its refresh
    // attempt included) has resolved, so no expected 401 is still to come.
    // From the login on, the session check must not need its refresh: a 401
    // from /me or /auth/refresh is a failure, the first check after the login
    // included.
    watch.signedIn = true;
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

    // The SPA never reads the session cookies' names, so nothing else notices
    // if gombit renames one. Checking them here fails the test on a rename,
    // rather than leave the expiry check below with nothing to look for.
    const signedInCookies = await page.context().cookies();
    expect(signedInCookies.map((c) => c.name), "signed in, the browser holds both session cookies").toEqual(
      expect.arrayContaining([accessCookie, refreshCookie]),
    );
    // Signed out, the session check's 401 is expected again.
    watch.signedIn = false;
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page.getByRole("form", { name: "Sign in" })).toBeVisible();

    // The UI goes anonymous as soon as the logout POST returns, whatever
    // happened to the session, so check both halves of signing out. Checked
    // before any reload: a refused refresh also clears the cookies.
    // - Chromium honoured the logout response's expiring Set-Cookie headers.
    const left = (await page.context().cookies()).map((c) => c.name);
    expect(left, "the logout response expired the access cookie").not.toContain(accessCookie);
    expect(left, "the logout response expired the refresh cookie").not.toContain(refreshCookie);
    // - The server ended the session: the access cookie from before the
    //   sign-out no longer works.
    const replay = await playwright.request.newContext({ baseURL, storageState: { cookies: signedInCookies, origins: [] } });
    expect((await replay.get("/api/v1/me")).status(), "the old access cookie is dead server-side").toBe(401);
    await replay.dispose();
    // And a reload doesn't bring the session back.
    await page.reload();
    await expect(page.getByRole("form", { name: "Sign in" })).toBeVisible();

    await seeded.api.dispose();
  });
});

// watchForProblems records what the flow should never produce: a page error, a
// console error, a failed load, and any 4xx or 5xx response, the SPA's assets
// included. The one expected 4xx is the session check's: signed out, the
// editor's /me and its one refresh attempt answer 401, which is how it knows to
// show the sign-in form. The test sets signedIn while a session should exist,
// and a 401 from either is a problem then.
function watchForProblems(page: Page) {
  const watch = { signedIn: false, problems: [] as string[] };
  page.on("pageerror", (err) => watch.problems.push(`page error: ${err.message}`));
  page.on("console", (msg) => {
    // Chrome also logs every failed load and 4xx/5xx response as a console
    // error. The two listeners below judge those, with the URL and status.
    if (msg.type() === "error" && !msg.text().startsWith("Failed to load resource")) {
      watch.problems.push(`console error: ${msg.text()}`);
    }
  });
  page.on("response", (resp) => {
    if (resp.status() < 400) return;
    const path = new URL(resp.url()).pathname;
    const sessionCheck = path === "/api/v1/me" || path === "/api/v1/auth/refresh";
    if (resp.status() === 401 && sessionCheck && !watch.signedIn) return;
    const when = watch.signedIn ? " while signed in" : "";
    watch.problems.push(`${resp.request().method()} ${path} → ${resp.status()}${when}`);
  });
  page.on("requestfailed", (req) => {
    const reason = req.failure()?.errorText ?? "unknown error";
    // A reload or navigation cancels the page's in-flight loads: those were
    // abandoned, not failed.
    if (reason === "net::ERR_ABORTED") return;
    watch.problems.push(`${req.method()} ${new URL(req.url()).pathname} failed: ${reason}`);
  });
  return watch;
}

// reportProblemsFirst runs the test body, then fails if the watcher recorded
// anything. When the body itself fails, the usual cause is something the
// watcher already saw: a refused request leaves the UI waiting for an element
// that never appears. So the failure leads with what the watcher recorded, and
// the assertion's own error follows it.
async function reportProblemsFirst(problems: string[], body: () => Promise<void>) {
  try {
    await body();
  } catch (err) {
    if (problems.length === 0) throw err;
    const failure = err instanceof Error ? err.message : String(err);
    throw new Error(`The page went wrong first:\n  ${problems.join("\n  ")}\n\nThen the test failed:\n${failure}`, {
      cause: err,
    });
  }
  expect(problems, "no page errors, failed loads, 5xx, or unexpected 4xx").toEqual([]);
}

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
