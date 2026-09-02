import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import {
  getProjectSpec,
  listOrganizations,
  listProjects,
  type Organization,
  type Project,
  type ProjectSpec,
} from "../api/projects";

// The Data area (DESIGN.md §17). This foundation wires organization → project
// selection to the control plane and loads the selected project's spec; the
// editable resource tree (create/rename/delete) lands on top of it in the next
// issue. Until then the loaded resources are shown read-only.
export function DataArea() {
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [orgID, setOrgID] = useState<number | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState<number | null>(null);
  const [spec, setSpec] = useState<ProjectSpec | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fail = (err: unknown) => setError(err instanceof ApiError ? err.message : "Something went wrong");

  useEffect(() => {
    listOrganizations().then(setOrgs).catch(fail);
  }, []);

  useEffect(() => {
    setProjectID(null);
    setProjects([]);
    setSpec(null);
    if (orgID == null) return;
    listProjects(orgID).then(setProjects).catch(fail);
  }, [orgID]);

  useEffect(() => {
    setSpec(null);
    if (projectID == null) return;
    getProjectSpec(projectID).then(setSpec).catch(fail);
  }, [projectID]);

  return (
    <section aria-labelledby="area-data">
      <h2 id="area-data">Data</h2>

      <div className="toolbar">
        <label>
          Organization
          <select
            aria-label="Organization"
            value={orgID ?? ""}
            onChange={(e) => setOrgID(e.target.value ? Number(e.target.value) : null)}
          >
            <option value="">Select an organization…</option>
            {orgs.map((o) => (
              <option key={o.id} value={o.id}>
                {o.name}
              </option>
            ))}
          </select>
        </label>

        <label>
          Project
          <select
            aria-label="Project"
            value={projectID ?? ""}
            disabled={orgID == null}
            onChange={(e) => setProjectID(e.target.value ? Number(e.target.value) : null)}
          >
            <option value="">Select a project…</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      </div>

      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}

      <ResourceList projectSelected={projectID != null} spec={spec} />
    </section>
  );
}

function ResourceList({ projectSelected, spec }: { projectSelected: boolean; spec: ProjectSpec | null }) {
  if (!projectSelected) {
    return <p className="muted">Select a project to edit its data model.</p>;
  }
  const resources = spec?.resources ?? [];
  if (resources.length === 0) {
    return <p className="muted">No resources yet.</p>;
  }
  return (
    <ul className="resource-tree" aria-label="Resources">
      {resources.map((r) => (
        <li key={r.id}>
          <span className="resource-label">{r.label}</span>
          {r.label_plural && <span className="muted"> · {r.label_plural}</span>}
        </li>
      ))}
    </ul>
  );
}
