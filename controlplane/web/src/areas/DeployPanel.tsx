import { useCallback, useEffect, useState } from "react";
import { describeError } from "../api/client";
import {
  deployBuild,
  getDeployment,
  isDeploymentBlocked,
  isDeploymentTerminal,
  isPreviewEnvironment,
  listEnvironments,
  rollbackEnvironment,
  type Deployment,
  type Environment,
} from "../api/deploy";
import { AppLogViewer } from "./AppLogViewer";

// DeployPanel is the #105 deploy action: pick one of the project's Gombit Cloud
// environments, deploy a succeeded build's artifact to it, and watch the
// deployment through Cloud's lifecycle — including the destructive-migration hold.
//
// Everything here is pass-through (ADR-005 D2/D6): Cloud owns the environment set,
// the deployment state machine, health, rollback and the migration gate. Forge
// submits the build and renders whatever Cloud reports; it holds no deployment
// state of its own and never approves a migration (L9/L10) — when Cloud holds a
// deploy in blocked_pending_approval, the panel links the operator to Cloud and
// keeps polling so the SAME deployment is seen to resume once a human approves.
export function DeployPanel({
  projectID,
  cloudBuildID,
}: {
  projectID: number;
  // The Cloud build id of the selected succeeded build — what gets deployed.
  cloudBuildID: string;
}) {
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [selectedEnvID, setSelectedEnvID] = useState<string>("");
  const [deployment, setDeployment] = useState<Deployment | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Guards against a not-linked project (422): the whole panel is inapplicable.
  const [unavailable, setUnavailable] = useState<string | null>(null);

  // Load the project's Cloud environments. A project not linked to Cloud (422)
  // has no deploy targets — surface that as an inapplicable panel, not an error.
  useEffect(() => {
    let active = true;
    setEnvironments([]);
    setSelectedEnvID("");
    setDeployment(null);
    setError(null);
    setUnavailable(null);
    listEnvironments(projectID)
      .then((envs) => {
        if (!active) return;
        setEnvironments(envs);
        // Preselect a single environment so the target is unambiguous; with
        // several, the operator must pick one explicitly (no silent default).
        if (envs.length === 1) setSelectedEnvID(envs[0].id);
      })
      .catch((e) => {
        if (!active) return;
        // 422 (unprocessable) is "not linked to Cloud", a precondition, not a fault.
        if (typeof e === "object" && e && "status" in e && (e as { status?: number }).status === 422) {
          setUnavailable("This project isn't linked to a Gombit Cloud project yet, so it has no environments to deploy to.");
        } else {
          setError(describeError(e));
        }
      });
    return () => {
      active = false;
    };
  }, [projectID]);

  // Poll the current deployment until Cloud's lifecycle settles. blocked_pending_
  // approval is NOT terminal, so polling continues across the hold and the panel
  // observes the same deployment resume the moment the migration is approved in
  // Cloud — no client re-drive, no Forge-side approval.
  const deploymentID = deployment?.id ?? null;
  const deploymentEnv = deployment?.environment_id ?? null;
  useEffect(() => {
    if (deploymentID == null || deploymentEnv == null) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = () => {
      getDeployment(projectID, deploymentEnv, deploymentID)
        .then((d) => {
          if (!active) return;
          setError(null);
          setDeployment(d);
          if (!isDeploymentTerminal(d.status)) timer = setTimeout(tick, 2000);
        })
        .catch((e) => {
          if (!active) return;
          setError(describeError(e));
          // A transient blip shouldn't freeze the view; back off and keep watching.
          timer = setTimeout(tick, 5000);
        });
    };
    timer = setTimeout(tick, 2000);
    return () => {
      active = false;
      if (timer) clearTimeout(timer);
    };
    // Keyed on the deployment id/env, not the whole deployment: a new deploy
    // (id change) re-arms the poll, but a status advance from the tick loop must
    // not, or it would cancel and restart the in-flight poll every 2s.
  }, [projectID, deploymentID, deploymentEnv]);

  const selectedEnv = environments.find((e) => e.id === selectedEnvID) ?? null;

  const onDeploy = useCallback(async () => {
    if (!selectedEnv) return;
    setBusy(true);
    setError(null);
    try {
      const d = await deployBuild(projectID, selectedEnv.id, cloudBuildID);
      setDeployment(d);
    } catch (e) {
      setError(describeError(e));
    } finally {
      setBusy(false);
    }
  }, [projectID, selectedEnv, cloudBuildID]);

  const onRollback = useCallback(async () => {
    if (!selectedEnv) return;
    setBusy(true);
    setError(null);
    try {
      const d = await rollbackEnvironment(projectID, selectedEnv.id);
      setDeployment(d);
    } catch (e) {
      setError(describeError(e));
    } finally {
      setBusy(false);
    }
  }, [projectID, selectedEnv]);

  if (unavailable) {
    return (
      <div className="deploy-panel" aria-label="Deploy">
        <p className="muted">{unavailable}</p>
      </div>
    );
  }

  return (
    <div className="deploy-panel" aria-label="Deploy">
      <h4>Deploy</h4>

      {environments.length === 0 ? (
        <p className="muted">No environments on the linked Cloud project yet.</p>
      ) : (
        <div className="deploy-target">
          <label htmlFor="deploy-env">Target environment</label>
          <select
            id="deploy-env"
            value={selectedEnvID}
            onChange={(e) => setSelectedEnvID(e.target.value)}
            disabled={busy}
          >
            <option value="">Choose an environment…</option>
            {environments.map((env) => (
              <option key={env.id} value={env.id}>
                {env.name}
                {isPreviewEnvironment(env) ? " · preview" : env.kind ? ` (${env.kind})` : ""}
              </option>
            ))}
          </select>
          <div className="deploy-actions">
            <button type="button" onClick={onDeploy} disabled={busy || !selectedEnv}>
              {selectedEnv ? `Deploy build to ${selectedEnv.name}` : "Deploy build"}
            </button>
            <button type="button" className="secondary" onClick={onRollback} disabled={busy || !selectedEnv}>
              {selectedEnv ? `Roll back ${selectedEnv.name}` : "Roll back"}
            </button>
          </div>
        </div>
      )}

      {selectedEnv && isPreviewEnvironment(selectedEnv) && (
        <p className="muted preview-note">
          Preview environment — throwaway, isolated data
          {selectedEnv.expires_at ? `, expires ${formatTime(selectedEnv.expires_at)}` : ""}.
        </p>
      )}

      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}

      {deployment && <DeploymentStatus deployment={deployment} />}

      {/* Application logs make sense once a deployment exists and isn't merely
          held awaiting approval (nothing is running yet during the hold). */}
      {deployment && !isDeploymentBlocked(deployment) && (
        <AppLogViewer
          projectID={projectID}
          envID={deployment.environment_id}
          deploymentID={deployment.id}
          stopped={isDeploymentTerminal(deployment.status) && deployment.status !== "healthy"}
        />
      )}
    </div>
  );
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toISOString().replace("T", " ").replace("Z", "");
}

// DeploymentStatus renders Cloud's view of a deployment: the block banner when it
// is held on a migration, otherwise the lifecycle status and provenance.
function DeploymentStatus({ deployment }: { deployment: Deployment }) {
  if (isDeploymentBlocked(deployment)) {
    return <BlockedBanner deployment={deployment} />;
  }
  const terminal = isDeploymentTerminal(deployment.status);
  return (
    <div className="deployment-status" aria-label="Deployment status">
      <p aria-live="polite">
        <span className={`deploy-state state-${deployment.status}`}>{humanizeStatus(deployment.status)}</span>
        {!terminal && <span className="muted"> · in progress…</span>}
      </p>
      {deployment.rolled_back_from_id && (
        <p className="muted">Rollback — restoring an earlier revision's build.</p>
      )}
      {deployment.artifact_digest && <p className="muted">Artifact {deployment.artifact_digest}</p>}
    </div>
  );
}

// BlockedBanner is the destructive-migration hold UX. It explains WHY the deploy
// is paused, links the operator to Cloud to approve (Forge never approves — L10),
// and states that the deployment resumes automatically once approved (the panel
// keeps polling, so the same deployment is observed to advance).
function BlockedBanner({ deployment }: { deployment: Deployment }) {
  const block = deployment.block;
  return (
    <div className="deploy-block" role="alert" aria-label="Deployment blocked">
      <strong>Deployment blocked — human approval required.</strong>
      <p>
        This deploy contains a database migration that may cause data loss. Gombit Cloud requires a person to review and
        approve it before the deployment can proceed.
      </p>
      {block?.approval_url ? (
        <p>
          <a href={block.approval_url} target="_blank" rel="noreferrer">
            Review migration in Gombit Cloud →
          </a>
        </p>
      ) : (
        <p className="muted">Approve the migration in Gombit Cloud to continue.</p>
      )}
      <p className="muted" aria-live="polite">
        Waiting for approval… this deployment resumes automatically once the migration is approved.
      </p>
    </div>
  );
}

// humanizeStatus turns Cloud's status token into a short label. Unknown/future
// tokens fall back to the raw value (with underscores spaced), so a new Cloud
// state still renders sensibly rather than blank.
function humanizeStatus(status: string): string {
  const labels: Record<string, string> = {
    pending: "Pending",
    preparing: "Preparing",
    migration_check: "Checking migrations",
    migrating: "Applying migrations",
    starting: "Starting",
    health_check: "Health checking",
    promoting: "Promoting",
    healthy: "Live",
    migration_blocked: "Migration blocked",
    migration_failed: "Migration failed",
    startup_failed: "Startup failed",
    health_failed: "Health check failed",
    promotion_failed: "Promotion failed",
    cancelled: "Cancelled",
  };
  return labels[status] ?? status.replace(/_/g, " ");
}
