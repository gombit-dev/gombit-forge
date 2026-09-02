import { api } from "./client";

// Control-plane resources the editor reads. These mirror the Go wire types
// (contract.Data envelopes); only the fields the editor uses are typed.

export interface Organization {
  id: number;
  name: string;
  slug: string;
}

export interface Project {
  id: number;
  organization_id: number;
  name: string;
  slug: string;
  head_revision_id?: number | null;
}

// A resource as it appears in a ProjectSpec. The full spec has more, but the
// resource tree needs only identity and the human-facing labels (DESIGN.md
// §4.3).
export interface SpecResource {
  id: string;
  label: string;
  label_plural?: string;
  code_name: string;
  storage_name: string;
}

export interface ProjectSpec {
  resources?: SpecResource[] | null;
}

export const listOrganizations = () => api.get<Organization[]>("/organizations");

export const listProjects = (orgID: number) => api.get<Project[]>(`/organizations/${orgID}/projects`);

// getProjectSpec returns the head revision's spec, or null when the project has
// no revisions yet.
export const getProjectSpec = (projectID: number) => api.get<ProjectSpec | null>(`/projects/${projectID}/spec`);
