import { useEffect, useRef, useState } from "react";
import { describeError } from "../api/client";
import { getDeploymentLogs, type DeploymentLog } from "../api/deploy";

// The most recent lines kept in view. Application logs are unbounded (a running
// app emits continuously), so the tail is capped to keep the DOM and memory
// bounded — this is a deploy-tab tail, not a full observability UI (#72).
const MAX_LINES = 500;
const POLL_MS = 3000;

// AppLogViewer tails a deployment's application logs, read from Gombit Cloud
// (ADR-005 §24 — Forge relays, never collects/stores). It shows the four fields
// #72 asks for: timestamp, level (Cloud's stream channel), message, request_id.
//
// It tails incrementally with `since` (the last line's timestamp) and appends, so
// a long-lived deployment doesn't refetch its whole history each tick. Polling
// runs while mounted; a `stopped` deployment (terminal-failed) still gets a final
// fetch so its last lines are shown, then polling stops.
export function AppLogViewer({
  projectID,
  envID,
  deploymentID,
  stopped,
}: {
  projectID: number;
  envID: string;
  deploymentID: string;
  // The deployment has failed/cancelled — the app isn't producing new logs, so
  // fetch once more and stop polling.
  stopped: boolean;
}) {
  const [logs, setLogs] = useState<DeploymentLog[]>([]);
  const [error, setError] = useState<string | null>(null);
  // The tail cursor: the timestamp of the last line seen, sent as `since`.
  const since = useRef<string>("");

  // Reset when the deployment changes — a new deployment tails from scratch.
  useEffect(() => {
    setLogs([]);
    setError(null);
    since.current = "";
  }, [deploymentID]);

  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const tick = () => {
      getDeploymentLogs(projectID, envID, deploymentID, since.current || undefined)
        .then((lines) => {
          if (!active) return;
          setError(null);
          if (lines.length > 0) {
            since.current = lines[lines.length - 1].timestamp;
            setLogs((prev) => {
              const next = prev.concat(lines);
              return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
            });
          }
          // Keep tailing a live deployment; a stopped one has no more lines.
          if (!stopped) timer = setTimeout(tick, POLL_MS);
        })
        .catch((e) => {
          if (!active) return;
          setError(describeError(e));
          // A transient blip shouldn't end the tail; back off and retry unless stopped.
          if (!stopped) timer = setTimeout(tick, POLL_MS * 2);
        });
    };
    tick();
    return () => {
      active = false;
      if (timer) clearTimeout(timer);
    };
  }, [projectID, envID, deploymentID, stopped]);

  return (
    <div className="app-logs" aria-label="Application logs">
      <h5>Application logs</h5>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {logs.length === 0 ? (
        <p className="muted">No application logs yet.</p>
      ) : (
        <table className="app-log-table">
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">Level</th>
              <th scope="col">Message</th>
              <th scope="col">Request</th>
            </tr>
          </thead>
          <tbody>
            {logs.map((l, i) => (
              <tr key={`${l.timestamp}-${i}`}>
                <td className="log-time">{formatTime(l.timestamp)}</td>
                <td className={`log-level level-${(l.stream ?? "").toLowerCase()}`}>{l.stream ?? ""}</td>
                <td className="log-message">{l.message}</td>
                <td className="log-request muted">{l.request_id ?? ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toISOString().replace("T", " ").replace("Z", "");
}
