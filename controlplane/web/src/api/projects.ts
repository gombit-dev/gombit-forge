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

// One enum value on a field.
export interface EnumValue {
  value: string;
  label?: string;
}

// A field as it appears in a ProjectSpec's resource.
export interface SpecField {
  id: string;
  label: string;
  type: FieldType;
  code_name: string;
  storage_name: string;
  required?: boolean;
  unique?: boolean;
  default?: string | null;
  enum_values?: EnumValue[];
  target?: string; // belongs_to target resource id
}

// A resource's behavior settings (DESIGN.md §4.3).
export interface ResourceBehavior {
  create_enabled?: boolean;
  update_enabled?: boolean;
  delete_enabled?: boolean;
  admin_visible?: boolean;
  list_fields?: string[];
  searchable_fields?: string[];
  sortable_fields?: string[];
  filterable_fields?: string[];
}

// A resource as it appears in a ProjectSpec.
export interface SpecResource {
  id: string;
  label: string;
  label_plural?: string;
  code_name: string;
  storage_name: string;
  fields?: SpecField[];
  behavior?: ResourceBehavior;
}

// The MVP field types the field editor offers. belongs_to is created with the
// relationship editor (#46), so it is not in this list.
export type FieldType =
  | "string"
  | "text"
  | "integer"
  | "decimal"
  | "boolean"
  | "datetime"
  | "date"
  | "enum"
  | "belongs_to";

export const EDITABLE_FIELD_TYPES: FieldType[] = [
  "string",
  "text",
  "integer",
  "decimal",
  "boolean",
  "datetime",
  "date",
  "enum",
];

export interface ProjectSpec {
  resources?: SpecResource[] | null;
}

// A revision reference returned by a committed edit.
export interface RevisionRef {
  id: number;
  spec_hash: string;
  abi_class?: string;
}

// One dependency that blocks a deletion.
export interface DeletionBlocker {
  kind: string;
  message: string;
}

// The result of a delete: committed, or blocked with the concrete dependencies.
export interface DeleteResult {
  committed: boolean;
  revision_id?: number;
  had_extension: boolean;
  blockers?: DeletionBlocker[];
}

export const listOrganizations = () => api.get<Organization[]>("/organizations");

export const addResource = (projectID: number, label: string, labelPlural: string) =>
  api.post<RevisionRef>(`/projects/${projectID}/resources`, { label, label_plural: labelPlural });

export const renameResource = (projectID: number, resourceID: string, label: string, labelPlural: string) =>
  api.patch<RevisionRef>(`/projects/${projectID}/resources/${encodeURIComponent(resourceID)}`, {
    label,
    label_plural: labelPlural,
  });

export const deleteResource = (projectID: number, resourceID: string) =>
  api.delete<DeleteResult>(`/projects/${projectID}/resources/${encodeURIComponent(resourceID)}`);

// FieldInput is what the field editor sends; the backend mints the symbol.
export interface FieldInput {
  label: string;
  type: FieldType;
  required?: boolean;
  unique?: boolean;
  default?: string | null;
  enum_values?: EnumValue[];
}

const fieldPath = (projectID: number, resourceID: string) =>
  `/projects/${projectID}/resources/${encodeURIComponent(resourceID)}/fields`;

export const addField = (projectID: number, resourceID: string, input: FieldInput) =>
  api.post<RevisionRef>(fieldPath(projectID, resourceID), input);

export const updateField = (projectID: number, resourceID: string, fieldID: string, input: FieldInput) =>
  api.patch<RevisionRef>(`${fieldPath(projectID, resourceID)}/${encodeURIComponent(fieldID)}`, input);

export const deleteField = (projectID: number, resourceID: string, fieldID: string) =>
  api.delete<RevisionRef>(`${fieldPath(projectID, resourceID)}/${encodeURIComponent(fieldID)}`);

// RelationshipInput describes a belongs_to relationship to create. The has_many
// side is derived by the compiler from inverse_label.
export interface RelationshipInput {
  label: string;
  target: string;
  inverse_label?: string;
  required?: boolean;
}

export const addRelationship = (projectID: number, resourceID: string, input: RelationshipInput) =>
  api.post<RevisionRef>(`/projects/${projectID}/resources/${encodeURIComponent(resourceID)}/relationships`, input);

// updateBehavior replaces the resource's whole behavior — the client must send
// the complete object, not a delta (omitted settings reset).
export const updateBehavior = (projectID: number, resourceID: string, behavior: ResourceBehavior) =>
  api.patch<RevisionRef>(`/projects/${projectID}/resources/${encodeURIComponent(resourceID)}/behavior`, behavior);

export const listProjects = (orgID: number) => api.get<Project[]>(`/organizations/${orgID}/projects`);

// getProjectSpec returns the head revision's spec, or null when the project has
// no revisions yet.
export const getProjectSpec = (projectID: number) => api.get<ProjectSpec | null>(`/projects/${projectID}/spec`);
