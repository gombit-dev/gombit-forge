package gen

import "text/template"

var (
	listPageTemplate   = template.Must(template.New("listpage").Parse(listPageSrc))
	detailPageTemplate = template.Must(template.New("detailpage").Parse(detailPageSrc))
	formPageTemplate   = template.Must(template.New("formpage").Parse(formPageSrc))
	registryTemplate   = template.Must(template.New("registry").Parse(registrySrc))
)

// listPageSrc renders a resource list: a table populated from the collection
// GET, mirroring Gombit's own generated list page (openapi-fetch client,
// unwrap, the paths/schema types).
const listPageSrc = `{{.Banner}}
import { useEffect, useState } from "react";
import { Link } from "react-router";

import { useApiClient } from "../../api/client";
import { unwrap } from "../../api/generated/client";
import type { paths } from "../../api/generated/schema";

type ListResponse =
  paths["{{.CollectionPath}}"]["get"]["responses"][200]["content"]["application/json"];
type {{.Type}}Row = NonNullable<ListResponse["data"]>[number];

export function {{.Type}}ListPage() {
  const client = useApiClient();
  const [rows, setRows] = useState<{{.Type}}Row[]>([]);
  const [status, setStatus] = useState("Loading {{.Title}}…");

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const listed = await unwrap(await client.GET("{{.CollectionPath}}"));
        if (cancelled) {
          return;
        }
        const data = Array.isArray(listed.data) ? listed.data : [];
        setRows(data);
        setStatus(data.length === 0 ? "No {{.Title}} yet." : "");
      } catch (err: unknown) {
        if (cancelled) {
          return;
        }
        setStatus(err instanceof Error ? err.message : "request failed");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client]);

  return (
    <section>
      <h1>{{.Title}}</h1>
{{- if .Create}}
      <p>
        <Link to="/{{.RouteBase}}/new">New {{.Type}}</Link>
      </p>
{{- end}}
      <table>
        <thead>
          <tr>
            <th>id</th>
{{- range .Fields}}
            <th>{{.Label}}</th>
{{- end}}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={String(row.id)}>
              <td>
                <Link to={` + "`/{{.RouteBase}}/${row.id}`" + `}>{String(row.id)}</Link>
              </td>
{{- range .Fields}}
              <td>{String(row["{{.JSONName}}"] ?? "")}</td>
{{- end}}
            </tr>
          ))}
        </tbody>
      </table>
      {status ? <p>{status}</p> : null}
    </section>
  );
}
`

// detailPageSrc renders a single record fetched by id.
const detailPageSrc = `{{.Banner}}
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";

import { useApiClient } from "../../api/client";
import { unwrap } from "../../api/generated/client";
import type { paths } from "../../api/generated/schema";

type GetResponse =
  paths["{{.CollectionPath}}/{id}"]["get"]["responses"][200]["content"]["application/json"];
type {{.Type}}Record = NonNullable<GetResponse["data"]>;

export function {{.Type}}DetailPage() {
  const client = useApiClient();
  const { id = "" } = useParams();
  const [record, setRecord] = useState<{{.Type}}Record | null>(null);
  const [status, setStatus] = useState("Loading…");

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const got = await unwrap(
          await client.GET("{{.CollectionPath}}/{id}", { params: { path: { id } } }),
        );
        if (cancelled) {
          return;
        }
        setRecord(got.data ?? null);
        setStatus("");
      } catch (err: unknown) {
        if (cancelled) {
          return;
        }
        setStatus(err instanceof Error ? err.message : "request failed");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client, id]);

  return (
    <section>
      <h1>{{.Type}}</h1>
      <p>
        <Link to="/{{.RouteBase}}">Back to {{.Title}}</Link>
{{- if .Update}}
        {" · "}
        <Link to={` + "`/{{.RouteBase}}/${id}/edit`" + `}>Edit</Link>
{{- end}}
      </p>
      {record ? (
        <dl>
          <dt>id</dt>
          <dd>{String(record.id)}</dd>
{{- range .Fields}}
          <dt>{{.Label}}</dt>
          <dd>{String(record["{{.JSONName}}"] ?? "")}</dd>
{{- end}}
        </dl>
      ) : null}
      {status ? <p>{status}</p> : null}
    </section>
  );
}
`

// formPageSrc renders the write form. Its shape follows the resource's toggles:
// with both create and update it loads on an id and chooses POST or PUT; with
// only one, it is fixed to that operation, and the unused imports, load effect
// and branch are omitted so the module stays lint-clean.
const formPageSrc = `{{.Banner}}
import { useState{{if .Update}}, useEffect{{end}} } from "react";
import { useForm } from "react-hook-form";
import { Link, useNavigate{{if .Update}}, useParams{{end}} } from "react-router";

import { useApiClient } from "../../api/client";
import { applyContractErrors } from "../../api/formErrors";
import { unwrap } from "../../api/generated/client";

type {{.Type}}FormValues = {
{{- range .Fields}}
  {{.JSONName}}: {{.TSType}};
{{- end}}
};

const emptyValues: {{.Type}}FormValues = {
{{- range .Fields}}
  {{.JSONName}}: {{if eq .TSType "number"}}0{{else if eq .TSType "boolean"}}false{{else}}""{{end}},
{{- end}}
};

export function {{.Type}}FormPage() {
  const client = useApiClient();
  const navigate = useNavigate();
{{- if .Update}}
  const { id = "" } = useParams();
{{- end}}
{{- if and .Create .Update}}
  const editing = id !== "";
{{- else if .Update}}
  const editing = true;
{{- else}}
  const editing = false;
{{- end}}
  const [status, setStatus] = useState("");
  const {
    register,
    handleSubmit,
{{- if .Update}}
    reset,
{{- end}}
    setError,
    formState: { errors, isSubmitting },
  } = useForm<{{.Type}}FormValues>({ defaultValues: emptyValues });
{{- if .Update}}

  useEffect(() => {
    if (!editing) {
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const got = await unwrap(
          await client.GET("{{.CollectionPath}}/{id}", { params: { path: { id } } }),
        );
        if (cancelled || !got.data) {
          return;
        }
        const record = got.data;
        reset({
{{- range .Fields}}
          {{.JSONName}}: (record["{{.JSONName}}"] ?? emptyValues.{{.JSONName}}) as {{.TSType}},
{{- end}}
        });
      } catch (err: unknown) {
        if (!cancelled) {
          setStatus(err instanceof Error ? err.message : "request failed");
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client, editing, id, reset]);
{{- end}}

  async function onSubmit(values: {{.Type}}FormValues) {
    setStatus("");
    try {
{{- if and .Create .Update}}
      if (editing) {
        await unwrap(
          await client.PUT("{{.CollectionPath}}/{id}", { params: { path: { id } }, body: values }),
        );
      } else {
        await unwrap(await client.POST("{{.CollectionPath}}", { body: values }));
      }
{{- else if .Update}}
      await unwrap(
        await client.PUT("{{.CollectionPath}}/{id}", { params: { path: { id } }, body: values }),
      );
{{- else}}
      await unwrap(await client.POST("{{.CollectionPath}}", { body: values }));
{{- end}}
      navigate("/{{.RouteBase}}");
    } catch (err: unknown) {
      if (!applyContractErrors(setError, err)) {
        setStatus(err instanceof Error ? err.message : "request failed");
      }
    }
  }

  return (
    <section>
      <h1>{editing ? "Edit {{.Type}}" : "New {{.Type}}"}</h1>
      <p>
        <Link to="/{{.RouteBase}}">Back to {{.Title}}</Link>
      </p>
      <form onSubmit={handleSubmit(onSubmit)}>
{{- range .Fields}}
        <label>
          {{.Label}}
{{- if eq .Input "checkbox"}}
          <input type="checkbox" {...register("{{.JSONName}}")} />
{{- else if eq .Input "number"}}
          <input type="number" {...register("{{.JSONName}}", { setValueAs: (value) => (value === "" ? 0 : Number(value)){{if .Required}}, required: "{{.Label}} is required"{{end}} })} />
{{- else if eq .Input "select"}}
          <select {...register("{{.JSONName}}"{{if .Required}}, { required: "{{.Label}} is required" }{{end}})}>
            <option value="">—</option>
{{- range .Options}}
            <option value="{{.Value}}">{{if .Label}}{{.Label}}{{else}}{{.Value}}{{end}}</option>
{{- end}}
          </select>
{{- else}}
          <input type="text"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}} {...register("{{.JSONName}}"{{if .Required}}, { required: "{{.Label}} is required" }{{end}})} />
{{- end}}
        </label>
        {errors.{{.JSONName}}?.message ? <p>{errors.{{.JSONName}}.message}</p> : null}
{{- end}}
        <button type="submit" disabled={isSubmitting}>
          {editing ? "Save" : "Create"}
        </button>
      </form>
      {status ? <p>{status}</p> : null}
    </section>
  );
}
`

// registrySrc renders resources.tsx: the routes, navigation and branding the
// application shell consumes. Routes are RouteObjects so the scaffold router
// can spread them in.
const registrySrc = `{{.Banner}}
import type { RouteObject } from "react-router";

{{range .Resources -}}
import { {{.Type}}ListPage } from "./{{.Package}}/{{.Type}}ListPage";
import { {{.Type}}DetailPage } from "./{{.Package}}/{{.Type}}DetailPage";
{{if or .Create .Update -}}
import { {{.Type}}FormPage } from "./{{.Package}}/{{.Type}}FormPage";
{{end -}}
{{end}}
export type GeneratedResource = {
  slug: string;
  title: string;
  listPath: string;
};

export const generatedResources: GeneratedResource[] = [
{{- range .Resources}}
  { slug: "{{.Package}}", title: "{{.Title}}", listPath: "/{{.RouteBase}}" },
{{- end}}
];

export const generatedResourceRoutes: RouteObject[] = [
{{- range .Resources}}
  { path: "{{.RouteBase}}", element: <{{.Type}}ListPage /> },
{{- if .Create}}
  { path: "{{.RouteBase}}/new", element: <{{.Type}}FormPage /> },
{{- end}}
  { path: "{{.RouteBase}}/:id", element: <{{.Type}}DetailPage /> },
{{- if .Update}}
  { path: "{{.RouteBase}}/:id/edit", element: <{{.Type}}FormPage /> },
{{- end}}
{{- end}}
];
`
