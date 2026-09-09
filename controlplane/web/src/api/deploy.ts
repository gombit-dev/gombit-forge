import { api } from "./client";

// Cloud build/deploy resources the Deploy area reads. These mirror the Go wire
// types (contract.Data envelopes); only the fields the UI uses are typed. The
// control plane relays build logs from Gombit Cloud — Forge stores neither the
// build nor its logs (ADR-005 §24, D2).

// A Cloud build job: Forge's tracking record for a build it submitted to Cloud.
export interface BuildJob {
  id: number;
  project_id: number;
  // status is Forge's local job lifecycle: queued | running | succeeded | failed.
  status: string;
  // cloud_status is the last state observed from Cloud's own build state machine
  // (queued | preparing | building | … | succeeded | failed), once known.
  cloud_status?: string;
  cloud_build_id?: string;
  artifact_digest?: string;
  error?: string;
}

// One build log line relayed from Cloud.
export interface BuildLog {
  timestamp: string;
  stream?: string;
  message: string;
}

// A build job's status is terminal when its Forge lifecycle has settled — no
// more log lines or status changes will arrive, so polling can stop.
export function isBuildTerminal(status: string): boolean {
  return status === "succeeded" || status === "failed";
}

export const listBuildJobs = (projectID: number) => api.get<BuildJob[]>(`/projects/${projectID}/build-jobs`);

export const getBuildJob = (jobID: number) => api.get<BuildJob>(`/build-jobs/${jobID}`);

// getBuildJobLogs reads the job's Cloud build logs, optionally only those after
// `since` (an RFC3339 timestamp) for tailing.
export const getBuildJobLogs = (jobID: number, since?: string) =>
  api.get<BuildLog[]>(`/build-jobs/${jobID}/logs${since ? `?since=${encodeURIComponent(since)}` : ""}`);
