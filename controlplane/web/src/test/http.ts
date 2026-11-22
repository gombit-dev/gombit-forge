// jsonResponse is what a fetch stub answers with: a real Response, as the
// browser's fetch returns, with the JSON body the control plane would send.
// The typed client (api/client.ts) reads it as fetch's own (status, headers,
// body), so a stub must not hand back a look-alike object.
export function jsonResponse(status: number, body?: unknown): Response {
  if (status === 204 || body === undefined) {
    return new Response(null, { status });
  }
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
