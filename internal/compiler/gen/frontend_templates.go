package gen

import (
	"encoding/json"
	"text/template"
)

// frontendFuncs escapes untrusted spec strings (labels, enum values) for the
// JS/JSX context. json.Marshal of a string yields a valid JavaScript string
// literal — quoted, with ", \, control characters and <, >, & all escaped — so
// a label like `Items {n}` or `Day "start"` becomes an inert string rather than
// a JSX expression or a broken literal. Identifiers derived from validated
// code/storage names (Type, RouteBase, …) are already safe and are not escaped.
var frontendFuncs = template.FuncMap{
	"js": func(s string) string {
		out, err := json.Marshal(s)
		if err != nil {
			// A Go string always marshals; fall back to an empty literal rather
			// than emitting something unquoted.
			return `""`
		}
		return string(out)
	},
}

func mustTemplate(name, src string) *template.Template {
	return template.Must(template.New(name).Funcs(frontendFuncs).Parse(src))
}

var (
	tablePageTemplate  = mustTemplate("tablepage", tablePageSrc)
	detailPageTemplate = mustTemplate("detailpage", detailPageSrc)
	formPageTemplate   = mustTemplate("formpage", formPageSrc)
	registryTemplate   = mustTemplate("registry", registrySrc)
)

// tablePageSrc renders a resource_table page: a table populated from the
// collection GET, mirroring Gombit's own generated list page (openapi-fetch
// client, unwrap, the paths/schema types). It is page-driven — the component,
// title and columns come from the spec's resource_table page — while its row and
// "New" links target the bound resource's canonical detail/form routes.
const tablePageSrc = `{{.Banner}}
import { useEffect, useState } from "react";
import { Link } from "react-router";

import { useApiClient } from "../../api/client";
import { unwrap } from "../../api/generated/client";
import type { paths } from "../../api/generated/schema";

type ListResponse =
  paths["{{.CollectionPath}}"]["get"]["responses"][200]["content"]["application/json"];
type {{.Type}}Row = NonNullable<ListResponse["data"]>[number];

export function {{.Component}}() {
  const client = useApiClient();
  const [rows, setRows] = useState<{{.Type}}Row[]>([]);
  const [status, setStatus] = useState({{js (printf "Loading %s…" .Title)}});

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
        setStatus(data.length === 0 ? {{js (printf "No %s yet." .Title)}} : "");
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
      <h1>{ {{js .Title}} }</h1>
{{- if .Create}}
      <p>
        <Link to="/{{.RouteBase}}/new">New {{.Type}}</Link>
      </p>
{{- end}}
      <table>
        <thead>
          <tr>
            <th>id</th>
{{- range .Columns}}
            <th>{ {{js .Label}} }</th>
{{- end}}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={String(row.id)}>
              <td>
                <Link to={` + "`/{{.RouteBase}}/${row.id}`" + `}>{String(row.id)}</Link>
              </td>
{{- range .Columns}}
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

// detailPageSrc renders a single record fetched by id. Its "back to list" link
// targets the resource's first table page and is omitted when it has none.
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
{{- if or .ListRoute .Update}}
      <p>
{{- if .ListRoute}}
        <Link to="/{{.ListRoute}}">Back to { {{js .Title}} }</Link>
{{- end}}
{{- if and .ListRoute .Update}}
        {" · "}
{{- end}}
{{- if .Update}}
        <Link to={` + "`/{{.RouteBase}}/${id}/edit`" + `}>Edit</Link>
{{- end}}
      </p>
{{- end}}
      {record ? (
        <dl>
          <dt>id</dt>
          <dd>{String(record.id)}</dd>
{{- range .Fields}}
          <dt>{ {{js .Label}} }</dt>
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
// and branch are omitted so the module stays lint-clean. Empty date/datetime
// values with a wire type that rejects "" (time.Time, decimal.Decimal) are
// dropped from the request body, since the API rejects
// "" (only RFC 3339 or a missing/null key unmarshal). On success it navigates
// to the resource's first table page, or the app root when it has none.
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
    const body = { ...values };
{{- range .Fields}}
{{- if .OmitEmpty}}
    if (body.{{.JSONName}} === "") {
      delete (body as Partial<{{$.Type}}FormValues>).{{.JSONName}};
    }
{{- end}}
{{- end}}
    try {
{{- if and .Create .Update}}
      if (editing) {
        await unwrap(
          await client.PUT("{{.CollectionPath}}/{id}", { params: { path: { id } }, body }),
        );
      } else {
        await unwrap(await client.POST("{{.CollectionPath}}", { body }));
      }
{{- else if .Update}}
      await unwrap(
        await client.PUT("{{.CollectionPath}}/{id}", { params: { path: { id } }, body }),
      );
{{- else}}
      await unwrap(await client.POST("{{.CollectionPath}}", { body }));
{{- end}}
      navigate("{{if .ListRoute}}/{{.ListRoute}}{{else}}/{{end}}");
    } catch (err: unknown) {
      if (!applyContractErrors(setError, err)) {
        setStatus(err instanceof Error ? err.message : "request failed");
      }
    }
  }

  return (
    <section>
      <h1>{editing ? "Edit {{.Type}}" : "New {{.Type}}"}</h1>
{{- if .ListRoute}}
      <p>
        <Link to="/{{.ListRoute}}">Back to { {{js .Title}} }</Link>
      </p>
{{- end}}
      <form onSubmit={handleSubmit(onSubmit)}>
{{- range .Fields}}
        <label>
          { {{js .Label}} }
{{- if eq .Input "checkbox"}}
          <input type="checkbox" {...register("{{.JSONName}}")} />
{{- else if eq .Input "number"}}
          <input type="number" {...register("{{.JSONName}}", { setValueAs: (value) => (value === "" ? 0 : Number(value)){{if .Required}}, required: {{js (printf "%s is required" .Label)}}{{end}} })} />
{{- else if eq .Input "select"}}
          <select {...register("{{.JSONName}}"{{if .Required}}, { required: {{js (printf "%s is required" .Label)}} }{{end}})}>
            <option value="">—</option>
{{- range .Options}}
            <option value={{js .Value}}>{ {{if .Label}}{{js .Label}}{{else}}{{js .Value}}{{end}} }</option>
{{- end}}
          </select>
{{- else}}
          <input type="text"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}} {...register("{{.JSONName}}"{{if .Required}}, { required: {{js (printf "%s is required" .Label)}} }{{end}})} />
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

// registrySrc renders resources.tsx: the routes and metadata the application
// shell consumes. Table routes are page-driven (one per resource_table page);
// detail/form routes are resource-driven. Routes are RouteObjects so the
// scaffold router can spread them in; only the operations a resource enables are
// registered, and only resources with a table page appear in generatedResources
// (the nav metadata).
const registrySrc = `{{.Banner}}
import type { RouteObject } from "react-router";

{{range .Resources -}}
import { {{.Type}}DetailPage } from "./{{.Package}}/{{.Type}}DetailPage";
{{if or .Create .Update -}}
import { {{.Type}}FormPage } from "./{{.Package}}/{{.Type}}FormPage";
{{end -}}
{{end -}}
{{range .Tables -}}
import { {{.Component}} } from "./{{.Package}}/{{.Component}}";
{{end}}
export type GeneratedResource = {
  slug: string;
  title: string;
  listPath: string;
};

export const generatedResources: GeneratedResource[] = [
{{- range .Tables}}
  { slug: "{{.Slug}}", title: {{js .Title}}, listPath: "/{{.Slug}}" },
{{- end}}
];

export const generatedResourceRoutes: RouteObject[] = [
{{- range .Tables}}
  { path: "{{.Slug}}", element: <{{.Component}} /> },
{{- end}}
{{- range .Resources}}
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
