import { useEffect, useState } from "react";
import { describeError } from "../api/client";
import { getBuildJob, getBuildJobLogs, isBuildTerminal, listBuildJobs, type BuildJob, type BuildLog } from "../api/deploy";
import { ProjectPicker } from "./ProjectPicker";

// The Deploy area's build-log view (#104): pick a project, see its Cloud build
// history, and live-tail the selected build's logs. Forge reads the logs from
// Gombit Cloud through the control plane and never stores them (ADR-005 §24); the
// deploy action itself and the destructive-migration approval UX are #105.
export function DeployArea() {
  const [projectID, setProjectID] = useState<number | null>(null);
  const [jobs, setJobs] = useState<BuildJob[]>([]);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [logs, setLogs] = useState<BuildLog[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Load the project's build history on a project change. Newest first (the API
  // orders it), so the most recent build is auto-selected.
  useEffect(() => {
    setError(null);
    setJobs([]);
    setSelectedID(null);
    setLogs([]);
    if (projectID == null) return;
    let active = true;
    listBuildJobs(projectID)
      .then((js) => {
        if (!active) return;
        setJobs(js);
        if (js.length > 0) setSelectedID(js[0].id);
      })
      .catch((e) => active && setError(describeError(e)));
    return () => {
      active = false;
    };
  }, [projectID]);

  // Live-tail the selected build: poll its logs and refreshed status until the
  // job settles, then stop. Re-running on selectedID means switching builds tails
  // the new one and cancels the old poll.
  //
  // Logs are re-fetched whole each tick rather than tailed with `since`: build
  // logs are bounded, and a full refresh sidesteps the dedup/ordering an append
  // would need — a deliberate simplicity choice, with `since` available on the
  // endpoint for a future incremental tail.
  useEffect(() => {
    if (selectedID == null) {
      setLogs([]);
      return;
    }
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const tick = () => {
      Promise.all([getBuildJob(selectedID), getBuildJobLogs(selectedID)])
        .then(([job, lines]) => {
          if (!active) return;
          setError(null);
          setLogs(lines);
          // Reflect the possibly-advanced status back into the list.
          setJobs((prev) => prev.map((j) => (j.id === job.id ? job : j)));
          if (!isBuildTerminal(job.status)) {
            timer = setTimeout(tick, 2000);
          }
        })
        .catch((e) => {
          if (!active) return;
          setError(describeError(e));
          // A transient failure shouldn't freeze the tail: keep polling on a
          // longer backoff so an in-progress build resumes once the blip clears.
          timer = setTimeout(tick, 5000);
        });
    };
    tick();
    return () => {
      active = false;
      if (timer) clearTimeout(timer);
    };
  }, [selectedID]);

  const selected = jobs.find((j) => j.id === selectedID) ?? null;

  return (
    <section aria-labelledby="area-deploy">
      <h2 id="area-deploy">Deploy</h2>
      <ProjectPicker onSelect={setProjectID} />

      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}

      {projectID != null && (
        <div className="deploy-area">
          <ul className="build-jobs" aria-label="Build history">
            {jobs.length === 0 ? (
              <li className="muted">No builds yet. Deploy this project to build it on Gombit Cloud.</li>
            ) : (
              jobs.map((j) => (
                <li key={j.id}>
                  <button
                    type="button"
                    className={j.id === selectedID ? "selected" : ""}
                    aria-current={j.id === selectedID}
                    onClick={() => setSelectedID(j.id)}
                  >
                    <span className={`build-status status-${j.status}`}>{j.status}</span>
                    <span className="build-id">build #{j.id}</span>
                    {j.cloud_status && <span className="muted">{j.cloud_status}</span>}
                  </button>
                </li>
              ))
            )}
          </ul>

          {selected && (
            <div className="build-detail" aria-label={`Build #${selected.id}`}>
              <header>
                <span className={`build-status status-${selected.status}`}>{selected.status}</span>
                {selected.cloud_status && <span className="muted"> · cloud: {selected.cloud_status}</span>}
                {selected.artifact_digest && <span className="muted"> · {selected.artifact_digest}</span>}
                {selected.error && (
                  <span role="alert" className="error">
                    {" "}
                    · {selected.error}
                  </span>
                )}
              </header>
              <pre className="build-logs" aria-label="Build logs" tabIndex={0}>
                {logs.length === 0
                  ? isBuildTerminal(selected.status)
                    ? "No logs."
                    : "Waiting for logs…"
                  : logs.map((l, i) => (
                      <span key={i} className="log-line">
                        {formatTime(l.timestamp)}
                        {l.stream ? ` [${l.stream}]` : ""} {l.message}
                        {"\n"}
                      </span>
                    ))}
              </pre>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toISOString().replace("T", " ").replace("Z", "");
}
