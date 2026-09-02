import { LitElement, html, css, nothing } from 'lit';
import { Task } from '@lit/task';
import { getStatus, getItems, getExceptions, getItem, getItemEvidence, getProvenance, getSessions, openIngress } from './api.js';
import { ActivityController } from './sse.js';

/** age renders a duration at the precision an operator cares about. */
export function age(sec) {
  if (sec == null) return '';
  if (sec < 90) return `${sec}s`;
  if (sec < 5400) return `${Math.floor(sec / 60)}m`;
  if (sec < 172800) return `${Math.floor(sec / 3600)}h`;
  return `${Math.floor(sec / 86400)}d`;
}

/**
 * describe renders a record's body as the one line a watcher wants.
 *
 * Bodies arrive base64-encoded — the chain endpoint serves raw evidence
 * records and Go marshals `[]byte` that way — and are JSON for structured
 * records, prose for reasons and notes. Anything unreadable falls back to the
 * subject rather than showing the operator an encoded blob.
 */
export function describe(r) {
  let body = '';
  try { body = atob(r.Body ?? ''); } catch { return r.Subject ?? ''; }
  if (!body) return r.Subject ?? '';
  let o = null;
  try { o = JSON.parse(body); } catch { /* prose, not JSON */ }
  if (o && typeof o === 'object') {
    switch (r.Kind) {
      case 'gate':
        return `${o.Gate}: ${o.Decision}${o.Reason ? ' — ' + firstLine(o.Reason) : ''}`;
      case 'agent-run':
        return `${o.seat} as ${o.role}, mode ${o.mode}`;
      case 'agent-result': {
        const bits = [`${o.stop}`, `${Math.round((o.wall_ms ?? 0) / 100) / 10}s`];
        // Unmetered is the fact worth seeing: an unreported cost is not a
        // zero cost, and it halts the window.
        bits.push(o.metered ? `${fmtTokens(o.tokens)} tokens` : 'cost NOT reported');
        // The agent's own figure in money, when it gave one. Tokens are the
        // brake; dollars are what the invoice is actually denominated in.
        if (o.cost_usd) bits.push(`$${o.cost_usd.toFixed(4)}`);
        // Which tools, not how many — a count cannot tell an ordinary run
        // from a runaway, because both are a number.
        bits.push(o.tools ? tools(o.tools) : `${o.tool_calls} tools`);
        if (o.denied_tools) bits.push(`DENIED ${tools(o.denied_tools)}`);
        if (o.unknown_updates?.length) bits.push(`dialect drift: ${o.unknown_updates.join(', ')}`);
        return bits.join(' · ');
      }
      case 'authz':
        return o.reason ?? '';
      default:
        return firstLine(body);
    }
  }
  return firstLine(body);
}

function firstLine(s) {
  const i = String(s).indexOf('\n');
  return i < 0 ? String(s) : String(s).slice(0, i);
}

/** A tool tally as one line, busiest first — the runaway signature is a name
 *  with a big number next to it. */
function tools(t) {
  return Object.entries(t)
    .sort((a, b) => b[1] - a[1])
    .map(([name, n]) => (n > 1 ? `${name}×${n}` : name))
    .join(' ');
}

/** Tokens at a glance: an operator reads 1.2M, not 1203481. */
export function fmtTokens(n) {
  if (n == null) return '0';
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${Math.round(n / 100) / 10}k`;
  return `${Math.round(n / 100_000) / 10}M`;
}

/** Where a unit came from, as one readable string. */
function origin(e) {
  if (e.route) return e.key ? `${e.route}/${e.key}` : e.route;
  return e.ingress ?? '';
}

// The states an operator sorts work into. `live` is the default because the
// question the room exists to answer is what is happening now, not what has
// ever happened.
// `handedoff` belongs in `live` and not in the parked lane: a session asked
// for a fresh one and the item is between them, needing nobody. Left out, work
// would vanish from the default view at the moment an agent said it was not
// finished — which is the reading an operator must never be given.
const FILTERS = {
  live: ['pending', 'claimed', 'waiting', 'handedoff'],
  ready: ['pending', 'handedoff'],
  blocked: ['pending', 'handedoff', 'waiting'],
  pending: ['pending'],
  claimed: ['claimed'],
  waiting: ['waiting'],
  handedoff: ['handedoff'],
  parked: ['parked'],
  done: ['done'],
  all: [],
};

// The two lanes a state cannot name. Ready and blocked are both `pending` on
// the wire — which is exactly why the frontier had to be a field: a queue of
// pending work and a queue of runnable work look identical, and only the
// second says whether the factory could be doing something.
const WHERE = {
  ready: (e) => !!e.ready,
  blocked: (e) => (e.blocked_by?.length ?? 0) > 0,
};

/** hold renders who has an item and how much of their claim is left. */
function hold(e) {
  if (!e.runner) return '';
  const left = e.lease_expires_sec ?? 0;
  return e.lease_lapsed
    ? `${e.runner} — lease LAPSED ${age(-left)} ago`
    : `${e.runner} — ${age(left)} left`;
}

/** why renders the graph reason a row is not moving: its unsettled
 *  dependencies, or the item a cascade actually started at. */
function why(e) {
  if (e.blocked_by?.length) return e.blocked_by.join(', ');
  if (e.root) return `cascaded from ${e.root}`;
  return '';
}

// The table's columns, each with the value it sorts on. A column is sortable
// or it is not a table — an operator hunting the oldest ready item, or every
// row one dead runner is holding, is sorting.
const COLUMNS = [
  { key: 'state', label: 'State', get: (e) => e.state },
  { key: 'work', label: 'Work', get: (e) => e.task || e.id },
  { key: 'why', label: 'Blocked by', get: (e) => why(e) },
  { key: 'stage', label: 'Stage', get: (e) => e.stage ?? '' },
  { key: 'holder', label: 'Held by', get: (e) => e.runner ?? '' },
  { key: 'origin', label: 'Origin', get: (e) => origin(e) },
  { key: 'age', label: 'Age', get: (e) => e.age_sec ?? 0 },
];

const KIND_APPEARANCE = {
  'ingress-open': 'neutral', queued: 'neutral', claimed: 'neutral',
  built: 'neutral', gate: 'neutral', land: 'positive', done: 'positive',
  check: 'positive', parked: 'negative', failed: 'negative',
  'budget-refused': 'negative', stop: 'negative', resume: 'positive',
  'config-reloaded': 'positive', spend: 'neutral', indexed: 'neutral',
};

/** Dashboard: the status card and the live activity feed. */
export class BoxDashboard extends LitElement {
  static styles = css`
    :host { display: grid; gap: 16px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; }
    .stat { text-align: center; }
    .stat .n { font-size: 1.6rem; font-weight: 600; }
    .stat .l { color: var(--jh-color-content-secondary-enabled); font-size: .8rem; }
    .feed { max-height: 420px; overflow-y: auto; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); }
    .idle { color: var(--jh-color-content-secondary-enabled); }
  `;

  _status = new Task(this, {
    task: async (_, { signal }) => getStatus(signal),
    args: () => [],
  });
  _running = new Task(this, {
    task: async (_, { signal }) => getItems(FILTERS.live, signal),
    args: () => [],
  });
  _activity = new ActivityController(this, { onEvent: () => this._refresh() });

  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => { this._status.run(); this._running.run(); }, 400);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
  }

  render() {
    return html`
      <jh-card header-title="Factory status" padding="medium">
        ${this._status.render({
          pending: () => html`<jh-progress accessible-label="Loading status"></jh-progress>`,
          error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
          complete: (s) => html`
            <div class="grid">
              <div class="stat"><div class="n">${s.pending}</div><div class="l">pending</div></div>
              <div class="stat"><div class="n">${s.exceptions}</div><div class="l">exceptions</div></div>
              ${/* The count's missing half. A queue drained briskly except for
                   the one item nobody wants looks healthy by every other
                   measure, so the oldest is the number on the wall. */ ''}
              ${s.exception_queue?.parked
                ? html`<div class="stat">
                    <div class="n">${age(s.exception_queue.oldest_age_sec)}</div>
                    <div class="l">oldest parked of ${age(s.exception_queue.objective_sec)}</div>
                  </div>`
                : nothing}
              <div class="stat"><div class="n">${s.stopped ? 'stopped' : 'running'}</div><div class="l">claims</div></div>
              ${s.spend ? html`
                <div class="stat">
                  <div class="n">${fmtTokens(s.spend.tokens)}</div>
                  <div class="l">tokens${s.spend.max_tokens ? ` of ${fmtTokens(s.spend.max_tokens)}` : ''}</div>
                </div>
                <div class="stat">
                  <div class="n">${s.spend.running}/${s.spend.max_concurrent}</div>
                  <div class="l">running</div>
                </div>` : nothing}
              <div class="stat"><div class="n">${s.routes.length}</div><div class="l">routes</div></div>
            </div>
            ${/* The total alone is a lie whenever this number is not zero, so
                 it is never shown on its own. */ ''}
            ${s.spend?.unmetered
              ? html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>
                  ${s.spend.unmetered} run(s) this window did not report a cost — the spend above is
                  incomplete and claims are stopped until the window turns.
                </jh-notification>`
              : nothing}
            ${s.spend?.refusing && !s.spend.unmetered
              ? html`<jh-notification appearance="neutral" type="banner" hide-dismiss-button>
                  The meter is pacing work: the factory is idle because it is over budget, not because
                  there is nothing to do.
                </jh-notification>`
              : nothing}
            ${s.exception_queue && !s.exception_queue.met
              ? html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>
                  ${s.exception_queue.overdue} exception(s) have been parked longer than the
                  ${age(s.exception_queue.objective_sec)} response-time objective — the oldest for
                  ${age(s.exception_queue.oldest_age_sec)}.
                </jh-notification>`
              : nothing}`,
        })}
      </jh-card>
      <jh-card header-title="In flight" header-subtitle="What the factory is doing right now, unit by unit" padding="medium">
        ${this._running.render({
          pending: () => html`<jh-progress accessible-label="Loading in-flight work"></jh-progress>`,
          error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
          complete: (items) => (items?.length ?? 0) === 0
            ? html`<p class="idle">Nothing in flight.</p>`
            : html`<jh-list-group accessible-label="In-flight work">
                ${items.map((e) => html`
                  <jh-list-item
                    primary-text=${e.task || e.id}
                    secondary-text=${e.note || origin(e)}
                    primary-metadata=${e.stage ?? ''}
                    secondary-metadata=${age(e.in_state_sec) + ' in stage'}
                    show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${e.state}></jh-tag>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${'/items/' + e.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`,
        })}
      </jh-card>
      <jh-card header-title="Activity" header-subtitle="Everything the factory does, as it does it" padding="medium">
        <div class="live">${this._activity.connected ? 'live' : 'reconnecting…'}</div>
        <div class="feed">
          <jh-list-group accessible-label="Activity feed">
            ${this._activity.events.slice().reverse().map((a) => html`
              <jh-list-item
                primary-text=${a.subject}
                secondary-text=${a.scope}
                primary-metadata=${new Date(a.at).toLocaleTimeString()}
                show-divider>
                <jh-tag slot="jh-list-item-left" size="small" label=${a.kind}></jh-tag>
              </jh-list-item>`)}
          </jh-list-group>
        </div>
      </jh-card>
    `;
  }
}
customElements.define('box-dashboard', BoxDashboard);

/**
 * Work: the frontier — what is runnable, what is blocked, on what, and who
 * holds each claim.
 *
 * A table, deliberately, and not a node-link diagram. The graph is the
 * enhancement; the table is the feature, because it is keyboard-navigable,
 * legible to a screen reader, sortable, and shippable before any layout
 * decision is made. A diagram that is the ONLY way to see blocked work is a
 * barrier, so this stays the representation of record and the diagram lands
 * on top of the same rows.
 *
 * The room could show what was stuck and what had landed, and nothing in
 * between — `pending=14` was the whole view into the queue, and even once the
 * listing arrived a pending row could not say whether it was next or waiting
 * on something dead.
 */
export class BoxWork extends LitElement {
  static styles = css`
    :host { display: block; }
    .filters { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
    .filters button {
      font: inherit; font-size: .8rem; cursor: pointer; padding: 4px 10px; border-radius: 999px;
      border: 1px solid var(--jh-color-border-primary-enabled, #ccc);
      background: transparent; color: var(--jh-color-content-primary-enabled);
    }
    .filters button[aria-pressed="true"] {
      background: var(--jh-color-container-secondary-enabled); font-weight: 600;
    }
    .wrap { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; font-size: .85rem; }
    caption { text-align: left; padding-bottom: 8px; color: var(--jh-color-content-secondary-enabled); font-size: .8rem; }
    th, td { text-align: left; vertical-align: top; padding: 6px 10px 6px 0;
             border-bottom: 1px solid var(--jh-color-border-primary-enabled, #e0e0e0); }
    th { white-space: nowrap; }
    thead th button {
      font: inherit; font-weight: 600; cursor: pointer; padding: 0; border: 0;
      background: transparent; color: inherit;
    }
    thead th[aria-sort="ascending"] button::after { content: ' \\2191'; }
    thead th[aria-sort="descending"] button::after { content: ' \\2193'; }
    .note { white-space: pre-wrap; font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    .muted { color: var(--jh-color-content-secondary-enabled); }
    .ready { color: var(--jh-color-content-positive-enabled, #146c2e); font-weight: 600; }
    .lapsed { color: var(--jh-color-content-negative-enabled, #b3261e); font-weight: 600; }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;
  static properties = { _filter: { state: true }, _sort: { state: true }, _desc: { state: true } };

  constructor() {
    super();
    this._filter = 'live';
    // No sort until asked: the server's order is oldest first, which is the
    // order the factory would take the work in and the only one that is a
    // fact about the queue rather than a preference of the reader.
    this._sort = '';
    this._desc = false;
    this._activity = new ActivityController(this, { limit: 1, onEvent: () => this._refresh() });
  }

  _task = new Task(this, {
    task: async ([f], { signal }) => getItems(FILTERS[f], signal),
    args: () => [this._filter],
  });

  // The feed is chatty and the fold is cheap; coalesce a burst into one fetch.
  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => this._task.run(), 400);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
  }

  _sortBy(key) {
    if (this._sort === key) { this._desc = !this._desc; return; }
    this._sort = key; this._desc = false;
  }

  _rows(items) {
    const where = WHERE[this._filter];
    const rows = (items ?? []).filter((e) => (where ? where(e) : true));
    const col = COLUMNS.find((c) => c.key === this._sort);
    if (!col) return rows;
    const dir = this._desc ? -1 : 1;
    return rows.slice().sort((a, b) => {
      const x = col.get(a), y = col.get(b);
      if (typeof x === 'number' && typeof y === 'number') return (x - y) * dir;
      return String(x).localeCompare(String(y)) * dir;
    });
  }

  render() {
    return html`
      <jh-card header-title="Work" header-subtitle="The frontier: what is runnable, what is blocked, and on what" padding="medium">
        <div class="filters" role="group" aria-label="Filter work">
          ${Object.keys(FILTERS).map((f) => html`
            <button type="button" aria-pressed=${this._filter === f ? 'true' : 'false'}
              @click=${() => { this._filter = f; }}>${f}</button>`)}
        </div>
        <div class="live">${this._activity.connected ? 'live' : 'reconnecting…'}</div>
        ${this._task.render({
          pending: () => html`<jh-progress accessible-label="Loading work"></jh-progress>`,
          error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
          complete: (items) => this._table(this._rows(items)),
        })}
      </jh-card>`;
  }

  _table(rows) {
    if (rows.length === 0) return html`<p class="empty">Nothing in this lane.</p>`;
    return html`
      <div class="wrap">
        <table>
          <caption>${rows.length} item(s), ${this._sort ? `sorted by ${this._sort}` : 'oldest first'}</caption>
          <thead>
            <tr>${COLUMNS.map((c) => html`
              <th scope="col" aria-sort=${this._sort === c.key ? (this._desc ? 'descending' : 'ascending') : 'none'}>
                <button type="button" @click=${() => this._sortBy(c.key)}>${c.label}</button>
              </th>`)}
            </tr>
          </thead>
          <tbody>
            ${rows.map((e) => html`
              <tr>
                <td>
                  <jh-tag size="small" label=${e.state}></jh-tag>
                  ${e.ready ? html`<div class="ready">ready</div>` : nothing}
                </td>
                <th scope="row">
                  <a href=${'/items/' + e.id}>${e.task || e.id}</a>
                  ${e.note ? html`<div class="note">${e.note}</div>` : nothing}
                </th>
                <td>${why(e) || html`<span class="muted">—</span>`}</td>
                <td>${e.stage ?? ''}
                  ${e.stage ? html`<div class="muted">${age(e.in_state_sec)}</div>` : nothing}</td>
                <td class=${e.lease_lapsed ? 'lapsed' : ''}>${hold(e) || html`<span class="muted">—</span>`}</td>
                <td class="muted">${origin(e)}</td>
                <td>${age(e.age_sec)}</td>
              </tr>`)}
          </tbody>
        </table>
      </div>`;
  }
}
customElements.define('box-work', BoxWork);

// What a frame says, in one line. The kinds are the wire's strings: this is
// an HTTP client and the ACP runtime's constants are on the other side of a
// protocol that exists to keep the two apart.
function frameLine(f) {
  switch (f.kind) {
    case 'started': return 'agent started';
    case 'tool': return f.tool || '(unnamed — nothing for policy to match)';
    case 'denied': return `${f.tool} — REFUSED by policy at the stream`;
    case 'escalate': return f.reason;
    case 'handoff': return f.reason;
    default: return f.text ?? '';
  }
}

// The frame kinds an operator should notice without looking for them.
const FRAME_APPEARANCE = { denied: 'negative', escalate: 'negative', handoff: 'neutral', started: 'positive' };

/**
 * Sessions: one card per running unit — what it is, how far it has got, and
 * what its agent is doing right now.
 *
 * The view the room did not have. A claimed item said `state=claimed head=`
 * and nothing else for the whole length of a run, while the daemon knew the
 * seat, the composed layers, the stage, and every word the agent had said.
 *
 * Cards side by side rather than one merged feed, because fan-out is the case
 * a feed breaks on: several runs interleave into a stream that reads as noise,
 * and the question an operator has — is THIS unit moving — is then answered by
 * scrolling.
 */
export class BoxSessions extends LitElement {
  static styles = css`
    :host { display: block; }
    .grid { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); align-items: start; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .meta { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; font-size: .8rem;
            color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
    .layers { display: flex; gap: 4px; flex-wrap: wrap; margin-bottom: 8px; }
    .layer { font-size: .7rem; padding: 2px 6px; border-radius: 4px;
             background: var(--jh-color-container-secondary-enabled); }
    h4 { margin: 12px 0 4px; font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    ol { list-style: none; margin: 0; padding: 0; max-height: 320px; overflow-y: auto; font-size: .8rem; }
    li { display: grid; grid-template-columns: 62px 70px 1fr; gap: 8px; padding: 3px 0;
         border-bottom: 1px solid var(--jh-color-border-primary-enabled, #eee); }
    li .t { color: var(--jh-color-content-secondary-enabled); font-variant-numeric: tabular-nums; }
    li .k { font-weight: 600; }
    li .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    li.negative .k { color: var(--jh-color-content-negative-enabled, #b3261e); }
    .blind { color: var(--jh-color-content-secondary-enabled); font-style: italic; }
    code { font-size: .75rem; }
  `;
  static properties = { _frames: { state: true } };

  constructor() {
    super();
    // Frames arriving on the stream, keyed by unit, merged onto whatever the
    // last fetch returned. Without this a card would only move as fast as the
    // roster refetch, which is the reload the page exists to replace.
    this._frames = {};
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: () => this._refresh(),
      onSession: (f) => {
        this._frames = { ...this._frames, [f.item]: [...(this._frames[f.item] ?? []), f] };
      },
    });
  }

  _task = new Task(this, { task: async (_, { signal }) => getSessions(signal), args: () => [] });

  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => this._task.run(), 400);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
  }

  // The fetched timeline is the truth about the past; the streamed frames are
  // what has happened since. Deduped by sequence, which the server assigns —
  // a refetch mid-stream otherwise shows an operator the same tool call twice
  // and makes a quiet run look busy.
  _timeline(s) {
    const seen = new Set((s.timeline ?? []).map((f) => f.seq));
    return [...(s.timeline ?? []), ...(this._frames[s.id] ?? []).filter((f) => !seen.has(f.seq))];
  }

  render() {
    return html`
      <div class="live">${this._activity.connected ? 'live' : 'reconnecting…'}</div>
      ${this._task.render({
        pending: () => html`<jh-progress accessible-label="Loading sessions"></jh-progress>`,
        error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
        complete: (sessions) => (sessions?.length ?? 0) === 0
          ? html`<p class="empty">No unit is running. Claimed work appears here as it starts.</p>`
          : html`<div class="grid">${sessions.map((s) => this._card(s))}</div>`,
      })}`;
  }

  _card(s) {
    const frames = this._timeline(s);
    return html`
      <jh-card header-title=${s.task || s.id} header-subtitle=${origin(s)} padding="medium">
        <div class="meta">
          <jh-tag size="small" label=${s.state}></jh-tag>
          ${s.stage ? html`<jh-tag size="small" label=${s.stage}></jh-tag><span>${age(s.in_state_sec)} in stage</span>` : nothing}
          ${s.seat ? html`<span>seat <strong>${s.seat}</strong></span>` : nothing}
          ${s.agentref ? html`<code title=${s.agentref}>${s.agentref.slice(0, 12)}</code>` : nothing}
          ${s.head ? html`<a href=${'/provenance/' + s.head}><code>${s.head.slice(0, 12)}</code></a>` : nothing}
          <a href=${'/items/' + s.id}>${s.id}</a>
        </div>
        ${/* Which layers, in which order — the agentref is a digest and alone
             it answers nothing about what the agent actually was. */ ''}
        ${s.layers?.length
          ? html`<div class="layers">${s.layers.map((l) => html`
              <span class="layer" title=${`${l.source} ${l.digest}`}>${l.scope}:${l.name}</span>`)}</div>`
          : nothing}
        <h4>Timeline</h4>
        ${frames.length === 0
          ? (s.watching
              ? html`<p class="empty">Started; nothing said yet.</p>`
              : html`<p class="blind">No live stream here — another runner holds this, or the window has rolled.
                     Silence about the run, not an idle agent.</p>`)
          : html`<ol>${frames.map((f) => html`
              <li class=${FRAME_APPEARANCE[f.kind] ?? ''}>
                <span class="t">${new Date(f.at).toLocaleTimeString()}</span>
                <span class="k">${f.kind}</span>
                <span class="x">${frameLine(f)}</span>
              </li>`)}
            </ol>`}
      </jh-card>`;
  }
}
customElements.define('box-sessions', BoxSessions);

/**
 * Exceptions: what still needs a person.
 *
 * Loudest first, then oldest — the order a person should work the queue,
 * which is not the order it arrived in. The age is the part the room could
 * never show: a queue whose items do not age cannot be late, which sounds
 * like a good property and is the absence of one.
 */
export class BoxExceptions extends LitElement {
  static styles = css`
    :host { display: block; }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .overdue { color: var(--jh-color-content-negative-enabled, #b3261e); font-weight: 600; }
    .told { font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
  `;
  _task = new Task(this, { task: async (_, { signal }) => getExceptions(signal), args: () => [] });

  render() {
    return html`
      <jh-card header-title="Exception queue" header-subtitle="Parked work waiting on a person" padding="medium">
        ${this._task.render({
          pending: () => html`<jh-progress accessible-label="Loading exceptions"></jh-progress>`,
          error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
          complete: (items) => (items?.length ?? 0) === 0
            ? html`<p class="empty">Nothing here — the factory has nothing waiting on a person.</p>`
            : html`<jh-list-group accessible-label="Parked items">
                ${items.map((it) => html`
                  <jh-list-item primary-text=${it.id} secondary-text=${it.note}
                    secondary-metadata=${age(it.age_sec) + ' parked'} show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${it.severity ?? 'medium'}></jh-tag>
                    <div slot="jh-list-item-content">
                      ${it.overdue
                        ? html`<div class="overdue">past the response-time objective</div>`
                        : nothing}
                      ${it.escalations
                        ? html`<div class="told">escalated out of band ${it.escalations}×</div>`
                        : nothing}
                    </div>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${'/items/' + it.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`,
        })}
      </jh-card>`;
  }
}
customElements.define('box-exceptions', BoxExceptions);

/** One item: its state and its whole evidence chain. */
export class BoxItem extends LitElement {
  static properties = { itemId: { attribute: false } };
  static styles = css`
    :host { display: grid; gap: 16px; }
    pre { white-space: pre-wrap; word-break: break-all; font-size: .8rem;
          background: var(--jh-color-container-secondary-enabled); padding: 8px; border-radius: 6px; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;
  constructor() {
    super();
    // A run's own records — the agent starting, each gate verdict, an
    // escalation and its reason — now reach the feed, so this page follows a
    // unit as it works instead of being a snapshot you reload.
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (a.scope === `workitem/${this.itemId}`) this._refresh(); },
    });
  }

  _task = new Task(this, {
    task: async ([id], { signal }) => {
      const [item, evidence] = await Promise.all([getItem(id, signal), getItemEvidence(id, signal)]);
      return { item, evidence };
    },
    args: () => [this.itemId],
  });

  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => this._task.run(), 400);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
  }

  render() {
    return this._task.render({
      pending: () => html`<jh-progress accessible-label="Loading item"></jh-progress>`,
      error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
      complete: ({ item, evidence }) => html`
        <jh-card header-title=${item.id} header-subtitle=${item.task} padding="medium">
          <p><jh-tag size="small" label=${item.state}></jh-tag>
             ${item.head ? html` <a href=${'/provenance/' + item.head}><code>${item.head.slice(0, 12)}</code></a>` : nothing}</p>
          ${item.note ? html`<pre>${item.note}</pre>` : nothing}
        </jh-card>
        <jh-card header-title="Timeline" header-subtitle="What this run did, in order" padding="medium">
          <div class="live">${this._activity.connected ? 'live' : 'reconnecting…'}</div>
          <jh-list-group accessible-label="Run timeline">
            ${(evidence ?? []).map((r) => html`
              <jh-list-item
                primary-text=${r.Kind}
                secondary-text=${describe(r)}
                primary-metadata=${r.At ? new Date(r.At).toLocaleTimeString() : ''}
                secondary-metadata=${r.Actor}
                show-divider></jh-list-item>`)}
          </jh-list-group>
        </jh-card>`,
    });
  }
}
customElements.define('box-item', BoxItem);

/** Provenance: the audit query. */
export class BoxProvenance extends LitElement {
  static properties = { sha: { attribute: false } };
  static styles = css`:host { display: block; }`;
  _task = new Task(this, {
    task: async ([sha], { signal }) => (sha ? getProvenance(sha, signal) : []),
    args: () => [this.sha],
  });

  render() {
    return html`
      <jh-card header-title="Provenance" header-subtitle="Everything recorded about one commit" padding="medium">
        ${this._task.render({
          pending: () => html`<jh-progress accessible-label="Loading provenance"></jh-progress>`,
          error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
          complete: (recs) => (recs?.length ?? 0) === 0
            ? html`<p>Nothing recorded about <code>${this.sha}</code>.</p>`
            : html`<jh-list-group accessible-label="Provenance records">
                ${recs.map((r) => html`
                  <jh-list-item primary-text=${r.Kind} secondary-text=${r.Scope}
                    primary-metadata=${'seq ' + r.Seq} secondary-metadata=${r.Actor} show-divider></jh-list-item>`)}
              </jh-list-group>`,
        })}
      </jh-card>`;
  }
}
customElements.define('box-provenance', BoxProvenance);

/** Submit: the delegation verb as a form. */
export class BoxSubmit extends LitElement {
  static styles = css`
    :host { display: block; }
    form { display: grid; gap: 16px; max-width: 560px; }
    form .actions { padding-top: 8px; }
  `;
  static properties = {
    _routes: { state: true }, _route: { state: true }, _key: { state: true },
    _payload: { state: true }, _result: { state: true }, _error: { state: true },
    _pending: { state: true },
  };

  constructor() {
    super();
    this._routes = []; this._route = ''; this._key = ''; this._payload = '';
    this._result = null; this._error = ''; this._pending = false;
  }

  connectedCallback() {
    super.connectedCallback();
    getStatus().then((s) => {
      this._routes = s.routes;
      if (!this._route && s.routes.length) this._route = s.routes[0];
    }).catch((e) => { this._error = e.message; });
  }

  async _submit(e) {
    e.preventDefault();
    this._pending = true; this._error = ''; this._result = null;
    try {
      this._result = await openIngress(this._route, this._key, this._payload);
    } catch (err) {
      this._error = err.message;
    } finally {
      this._pending = false;
    }
  }

  render() {
    return html`
      <jh-card header-title="Submit work" header-subtitle="Open an ingress; the daemon's next tick takes it from there" padding="medium">
        <form @submit=${this._submit}>
          <jh-radio-group label="Route" name="route" .value=${this._route}
            helper-text="Routes are the operator's configuration; work inherits the route's target and authority"
            @jh-change=${(e) => { this._route = e.target.value; }}>
            ${this._routes.map((r) => html`<jh-radio label=${r} value=${r} ?checked=${r === this._route}></jh-radio>`)}
          </jh-radio-group>
          <jh-input label="Occurrence key" required
            helper-text="The same key always names the same work — redelivery is safe"
            .value=${this._key}
            @jh-change=${(e) => { this._key = e.target.value; }}
            @input=${(e) => { this._key = e.composedPath()[0].value ?? this._key; }}></jh-input>
          <jh-input type="textarea" label="Task" rows="3" auto-grow required
            helper-text="What should be done, in plain words"
            .value=${this._payload}
            @jh-change=${(e) => { this._payload = e.target.value; }}
            @input=${(e) => { this._payload = e.composedPath()[0].value ?? this._payload; }}></jh-input>
          <div class="actions">
            <jh-button submit appearance="primary" label="Open ingress"
              ?pending=${this._pending} ?disabled=${!this._route || !this._key || !this._payload}></jh-button>
          </div>
        </form>
        ${this._error ? html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${this._error}</jh-notification>` : nothing}
        ${this._result ? html`
          <jh-notification appearance="positive" type="banner" hide-dismiss-button>
            ${this._result.opened ? 'Opened' : 'Already open'} — item
            <a href=${'/items/' + this._result.item}>${this._result.item}</a>
          </jh-notification>` : nothing}
      </jh-card>`;
  }
}
customElements.define('box-submit', BoxSubmit);

export { KIND_APPEARANCE };
