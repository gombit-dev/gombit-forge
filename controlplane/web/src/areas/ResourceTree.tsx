import { useState } from "react";
import type { FormEvent } from "react";
import { ApiError } from "../api/client";
import {
  addResource,
  deleteResource,
  renameResource,
  type DeletionBlocker,
  type SpecResource,
} from "../api/projects";

// The editable resource tree (DESIGN.md §17 Data, §4.2). It edits resources
// through the backend operations — the backend mints code symbols and runs the
// F0 deletion dependency analysis; this only supplies human-readable labels.
// After any successful edit it calls onChanged so the parent reloads the spec.
export function ResourceTree({
  projectID,
  resources,
  onChanged,
}: {
  projectID: number;
  resources: SpecResource[];
  onChanged: () => void;
}) {
  const [error, setError] = useState<string | null>(null);

  const run = async (op: () => Promise<void>) => {
    setError(null);
    try {
      await op();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong");
    }
  };

  return (
    <div className="resource-editor">
      {resources.length === 0 ? (
        <p className="muted">No resources yet. Add the first one below.</p>
      ) : (
        <ul className="resource-tree" aria-label="Resources">
          {resources.map((r) => (
            <ResourceRow key={r.id} projectID={projectID} resource={r} onChanged={onChanged} setError={setError} />
          ))}
        </ul>
      )}

      <AddResourceForm
        onAdd={(label, plural) => run(async () => {
          await addResource(projectID, label, plural);
          onChanged();
        })}
      />

      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
    </div>
  );
}

function ResourceRow({
  projectID,
  resource,
  onChanged,
  setError,
}: {
  projectID: number;
  resource: SpecResource;
  onChanged: () => void;
  setError: (m: string | null) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [blockers, setBlockers] = useState<DeletionBlocker[] | null>(null);

  const fail = (err: unknown) => setError(err instanceof ApiError ? err.message : "Something went wrong");

  async function save(label: string, plural: string) {
    setError(null);
    try {
      await renameResource(projectID, resource.id, label, plural);
      setEditing(false);
      onChanged();
    } catch (err) {
      fail(err);
    }
  }

  async function confirmDelete() {
    setError(null);
    setBlockers(null);
    try {
      const result = await deleteResource(projectID, resource.id);
      if (result.committed) {
        onChanged();
      } else {
        // Blocked by dependencies (§45): show what still references the resource.
        setBlockers(result.blockers ?? []);
      }
    } catch (err) {
      fail(err);
    } finally {
      setConfirming(false);
    }
  }

  if (editing) {
    return (
      <li>
        <RenameForm
          resource={resource}
          onSave={save}
          onCancel={() => setEditing(false)}
        />
      </li>
    );
  }

  return (
    <li>
      <div className="resource-row">
        <span className="resource-label">{resource.label}</span>
        {resource.label_plural && <span className="muted"> · {resource.label_plural}</span>}
        <span className="row-actions">
          <button type="button" onClick={() => setEditing(true)}>
            Rename
          </button>
          <button type="button" onClick={() => setConfirming(true)}>
            Delete
          </button>
        </span>
      </div>

      {confirming && (
        <div className="confirm" role="group" aria-label={`Delete ${resource.label}`}>
          <span>Delete “{resource.label}”? Any custom code is archived at build time.</span>
          <button type="button" onClick={confirmDelete}>
            Confirm delete
          </button>
          <button type="button" onClick={() => setConfirming(false)}>
            Cancel
          </button>
        </div>
      )}

      {blockers && (
        <div className="blockers" role="alert">
          <p>Can’t delete “{resource.label}” — it is still referenced:</p>
          <ul>
            {blockers.map((b, i) => (
              <li key={i}>{b.message}</li>
            ))}
          </ul>
        </div>
      )}
    </li>
  );
}

function RenameForm({
  resource,
  onSave,
  onCancel,
}: {
  resource: SpecResource;
  onSave: (label: string, plural: string) => void;
  onCancel: () => void;
}) {
  const [label, setLabel] = useState(resource.label);
  const [plural, setPlural] = useState(resource.label_plural ?? "");
  return (
    <form
      className="resource-form"
      aria-label={`Rename ${resource.label}`}
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        onSave(label, plural);
      }}
    >
      <input aria-label="Label" value={label} onChange={(e) => setLabel(e.target.value)} required />
      <input aria-label="Plural label" value={plural} onChange={(e) => setPlural(e.target.value)} placeholder="Plural" />
      <button type="submit">Save</button>
      <button type="button" onClick={onCancel}>
        Cancel
      </button>
    </form>
  );
}

function AddResourceForm({ onAdd }: { onAdd: (label: string, plural: string) => void }) {
  const [label, setLabel] = useState("");
  const [plural, setPlural] = useState("");
  return (
    <form
      className="resource-form add"
      aria-label="Add resource"
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        onAdd(label, plural);
        setLabel("");
        setPlural("");
      }}
    >
      <input aria-label="New resource label" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Resource label" required />
      <input aria-label="New resource plural" value={plural} onChange={(e) => setPlural(e.target.value)} placeholder="Plural (optional)" />
      <button type="submit">Add resource</button>
    </form>
  );
}
