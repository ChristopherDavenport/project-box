// The control room's client: same-origin /v1, cookie-carried token.
// EventSource cannot set headers, so on first load a ?token= query is moved
// into a cookie and stripped from the URL — both vanish with OIDC.

export function bootstrapToken() {
  const url = new URL(location.href);
  const t = url.searchParams.get('token');
  if (t) {
    document.cookie = `box_token=${encodeURIComponent(t)}; path=/; SameSite=Strict`;
    url.searchParams.delete('token');
    history.replaceState({}, '', url.toString());
  }
}

/** ApiError carries the server's machine code beside its message, so a view
 *  can match `code` — the contract — and show `message` — the prose. */
export class ApiError extends Error {
  constructor(message, { status, code, method, path } = {}) {
    super(message);
    this.status = status;
    this.code = code ?? 'internal';
    this.method = method;
    this.path = path;
  }
}

async function req(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  if (!res.ok) {
    let msg = `${method} ${path}: ${res.status}`;
    let code;
    try {
      const e = await res.json();
      msg = e.error ?? msg;
      code = e.code;
    } catch { /* keep status text */ }
    throw new ApiError(msg, { status: res.status, code, method, path });
  }
  return res.json();
}

async function fetchJSON(path, signal) {
  const res = await fetch(path, { signal, credentials: 'same-origin' });
  if (!res.ok) {
    let msg = `${path}: ${res.status}`;
    let code;
    try {
      const e = await res.json();
      msg = e.error ?? msg;
      code = e.code;
    } catch { /* keep status text */ }
    throw new ApiError(msg, { status: res.status, code, method: 'GET', path });
  }
  return res.json();
}

export const getStatus = (signal) => fetchJSON('/v1/status', signal);
export const getItems = (states, signal) =>
  fetchJSON('/v1/items' + (states?.length ? `?state=${encodeURIComponent(states.join(','))}` : ''), signal);
export const getExceptions = (signal) => fetchJSON('/v1/exceptions', signal);
export const getItem = (id, signal) => fetchJSON(`/v1/items/${encodeURIComponent(id)}`, signal);
export const getItemEvidence = (id, signal) => fetchJSON(`/v1/items/${encodeURIComponent(id)}/evidence`, signal);
// An origination's own facts and where its judgement stands — where a
// judgement's session card links, since /v1/items/{id} 404s on an ing- id.
export const getIngress = (id, signal) => fetchJSON(`/v1/ingress/${encodeURIComponent(id)}`, signal);
export const getProvenanceChain = (sha, signal) => fetchJSON(`/v1/provenance/${encodeURIComponent(sha)}/chain`, signal);
export const provenanceExportURL = (sha) => `/v1/provenance/${encodeURIComponent(sha)}/export`;
export const getPlan = (ref, signal) => fetchJSON(`/v1/plans/${encodeURIComponent(ref)}`, signal);
export const getIngressPlans = (id, signal) => fetchJSON(`/v1/ingress/${encodeURIComponent(id)}/plans`, signal);
export const getPlanDiff = (ref, other, signal) =>
  fetchJSON(`/v1/plans/${encodeURIComponent(ref)}/diff/${encodeURIComponent(other)}`, signal);
export const getActivity = (signal) => fetchJSON('/v1/activity', signal);
export const getSessions = (signal) => fetchJSON('/v1/sessions', signal);
export const getSession = (id, signal) => fetchJSON(`/v1/items/${encodeURIComponent(id)}/session`, signal);
// The run log: what a run did, kept after it — a work item's id, or plan-<ingress> for a judgement.
export const getRunFrames = (run, signal) => fetchJSON(`/v1/runs/${encodeURIComponent(run)}/frames`, signal);

// The verbs. Every one that changes the factory carries a reason, because
// every one becomes a record and a record needs one.
export const openIngress = (route, key, payload) => req('POST', '/v1/ingress', { route, key, payload });
export const stop = (reason) => req('POST', '/v1/stop', { reason });
export const resume = (reason) => req('POST', '/v1/resume', { reason });
export const resolveItem = (id, reason) => req('POST', `/v1/items/${encodeURIComponent(id)}/resolve`, { reason });
export const retryItem = (id, reason) => req('POST', `/v1/items/${encodeURIComponent(id)}/retry`, { reason });
export const reload = () => req('POST', '/v1/reload', {});
