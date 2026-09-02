import type { ReactNode } from "react";

// The four editor areas (DESIGN.md §17). This is the single source of truth for
// navigation and routing, so the shell nav and the router can never disagree
// about which areas exist or their order.
export interface EditorArea {
  path: string;
  label: string;
  // summary describes what the area will hold; the concrete editors land in the
  // later M2/M3 issues, so today each area renders its summary as a placeholder.
  summary: string;
}

export const AREAS: EditorArea[] = [
  { path: "data", label: "Data", summary: "Resource tree and field editor." },
  { path: "pages", label: "Pages", summary: "Page list and structured page properties." },
  { path: "access", label: "Access", summary: "Users, groups and permission configuration." },
  { path: "deploy", label: "Deploy", summary: "Preview, build history, production deployment and logs." },
];

export const DEFAULT_AREA = AREAS[0];

// AreaPlaceholder is the interim view for an area whose editor is not built yet.
export function AreaPlaceholder({ area }: { area: EditorArea }): ReactNode {
  return (
    <section aria-labelledby={`area-${area.path}`}>
      <h2 id={`area-${area.path}`}>{area.label}</h2>
      <p>{area.summary}</p>
    </section>
  );
}
