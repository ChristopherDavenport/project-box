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

async function req(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  if (!res.ok) {
    let msg = `${method} ${path}: ${res.status}`;
    try { msg = (await res.json()).error ?? msg; } catch { /* keep status text */ }
    throw new Error(msg);
  }
  return res.json();
}

export const getStatus = (signal) => fetchJSON('/v1/status', signal);
export const getItems = (states, signal) =>
  fetchJSON('/v1/items' + (states?.length ? `?state=${encodeURIComponent(states.join(','))}` : ''), signal);
export const getExceptions = (signal) => fetchJSON('/v1/exceptions', signal);
export const getItem = (id, signal) => fetchJSON(`/v1/items/${encodeURIComponent(id)}`, signal);
export const getItemEvidence = (id, signal) => fetchJSON(`/v1/items/${encodeURIComponent(id)}/evidence`, signal);
export const getProvenance = (sha, signal) => fetchJSON(`/v1/provenance/${encodeURIComponent(sha)}`, signal);
export const getActivity = (signal) => fetchJSON('/v1/activity', signal);
export const getSessions = (signal) => fetchJSON('/v1/sessions', signal);
export const getSession = (id, signal) => fetchJSON(`/v1/items/${encodeURIComponent(id)}/session`, signal);
export const openIngress = (route, key, payload) => req('POST', '/v1/ingress', { route, key, payload });
export const stop = (reason) => req('POST', '/v1/stop', { reason });
export const resume = (reason) => req('POST', '/v1/resume', { reason });
export const reload = () => req('POST', '/v1/reload', {});

async function fetchJSON(path, signal) {
  const res = await fetch(path, { signal, credentials: 'same-origin' });
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  return res.json();
}
