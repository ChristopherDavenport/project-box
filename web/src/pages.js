// The views. Each is one question an operator brings to the room, answered
// in words: what is the factory doing, what could it be doing, what is each
// agent saying, what needs a person, what happened to this unit, what is
// recorded about this commit, and how do I hand it more work.
import { LitElement, html, css, nothing } from 'lit';
import { unsafeHTML } from 'lit/directives/unsafe-html.js';
import { Task } from '@lit/task';
import {
  getStatus, getItems, getExceptions, getItem, getItemEvidence, getIngress, getProvenance, getProvenanceChain, provenanceExportURL,
  getSessions, getSession, openIngress,getRunFrames, reload, retryItem, resolveItem,
  getPlan, getIngressPlans, getPlanDiff,
} from './api.js';
import { ActivityController } from './sse.js';
import {
  age, title, taskURL, short, fmtTokens, fmtUSD, tokenSplit, breakdown, origin, place, hold, why,
  describe, itemOf, appearanceOf, STATE_APPEARANCE, SEVERITY_APPEARANCE, firstLine, clip, toolDetail,
} from './format.js';
import { base, toast, focusInto } from './ui.js';
import { icon } from './icons.js';

// The states an operator sorts work into. `live` is the default because the
// question the room exists to answer is what is happening now, not what has
// ever happened.
// `handedoff` belongs in `live` and not in the parked lane: a session asked
// for a fresh one and the item is between them, needing nobody. Left out,
// work would vanish from the default view at the moment an agent said it was
// not finished — which is the reading an operator must never be given.
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

// The table's columns, each with the value it sorts on. A column is sortable
// or it is not a table — an operator hunting the oldest ready item, or every
// row one dead runner is holding, is sorting.
const COLUMNS = [
  { key: 'state', label: 'State', get: (e) => e.state },
  { key: 'work', label: 'Work', get: (e) => title(e) || e.id },
  { key: 'why', label: 'Blocked by', get: (e) => why(e) },
  { key: 'stage', label: 'Stage', get: (e) => e.stage ?? '' },
  { key: 'holder', label: 'Held by', get: (e) => e.runner ?? '' },
  { key: 'origin', label: 'Origin', get: (e) => origin(e) },
  { key: 'age', label: 'Age', get: (e) => e.age_sec ?? 0 },
];

const stateTag = (state) => html`<jh-tag size="small" class=${STATE_APPEARANCE[state] ?? 'neutral'} label=${state}></jh-tag>`;
// A judgement's standing, once it has one: plan.Store.Standing's own words,
// not the queue's — planned and resolved settled well, failed did not.
const STANDING_APPEARANCE = { planned: 'positive', resolved: 'positive', failed: 'negative', retried: 'brand' };
const kindTag = (kind) => html`<jh-tag size="small" class=${appearanceOf(kind)} label=${kind}></jh-tag>`;

const loading = (what) => html`<jh-progress accessible-label=${`Loading ${what}`}></jh-progress>`;
const failed = (e) => html`<jh-notification appearance="negative" type="alert" hide-dismiss-button>${e.message}</jh-notification>`;

// Pill-shaped chips — the one control the system does not ship, styled with
// its tokens and its focus ring so it reads as one of the family.
const chips = css`
  .chips { display: flex; gap: var(--jh-space-100); flex-wrap: wrap; }
  .chips button {
    font: var(--jh-font-helper-medium); cursor: pointer; padding: 4px var(--jh-space-300);
    border-radius: var(--jh-border-radius-pill); border: 1px solid var(--jh-color-control-enabled);
    background: transparent; color: var(--jh-color-content-primary-enabled);
  }
  .chips button:hover { background: var(--jh-color-container-secondary-hover); }
  .chips button[aria-pressed="true"] { background: var(--jh-color-container-secondary-selected); border-color: transparent; font-weight: 600; }
`;

// Facts as a definition list: the label beside the value, wrapping in pairs.
const facts = css`
  dl.facts { display: grid; grid-template-columns: max-content 1fr; gap: var(--jh-space-100) var(--jh-space-400); margin: 0; }
  dl.facts dt { font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
  dl.facts dd { margin: 0; overflow-wrap: anywhere; }
  @media (max-width: 560px) { dl.facts { grid-template-columns: 1fr; } dl.facts dd { margin-bottom: var(--jh-space-200); } }
`;

/** Dashboard: the numbers, what they are made of, and the feed. */
export class BoxDashboard extends LitElement {
  static styles = [base, facts, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: var(--jh-space-300); margin-bottom: var(--jh-space-400); }
    .tile { display: block; padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
            background: var(--jh-color-container-secondary-enabled); color: inherit; text-decoration: none; }
    a.tile:hover { background: var(--jh-color-container-secondary-hover); }
    .tile .n { font: var(--jh-font-heading-medium-4); }
    .tile .l { font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    .tile .s { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .tile.negative .n { color: var(--jh-color-content-negative-enabled); }
    .tile.positive .n { color: var(--jh-color-content-positive-enabled); }
    .foot { display: flex; align-items: center; gap: var(--jh-space-300); flex-wrap: wrap; margin-top: var(--jh-space-400);
            padding-top: var(--jh-space-300); border-top: 1px solid var(--jh-color-divider-secondary); }
    ol.feed { list-style: none; margin: 0; padding: 0; max-height: 480px; overflow-y: auto; }
    ol.feed li { display: grid; grid-template-columns: minmax(92px, max-content) 1fr max-content; gap: var(--jh-space-300);
                 align-items: baseline; padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.feed .what { overflow-wrap: anywhere; }
    ol.feed .by { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    ol.feed .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    .row { display: flex; gap: var(--jh-space-300); align-items: center; flex-wrap: wrap; margin-bottom: var(--jh-space-300); }
  `];

  _status = new Task(this, { task: async (_, { signal }) => getStatus(signal), args: () => [] });
  // Every item, once: the live list is a filter over it, and the titles that
  // let the feed say "Landed: <task>" instead of "indexed wi-ing-… index/landings".
  _items = new Task(this, { task: async (_, { signal }) => getItems([], signal), args: () => [] });
  _activity = new ActivityController(this, { onEvent: () => this._refresh() });

  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => { this._status.run(); this._items.run(); }, 400);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
  }

  async _reload() {
    try {
      const r = await reload();
      toast(`Configuration reloaded: ${r.reloaded}`, 'positive');
    } catch (e) {
      toast(e.message, 'negative', 10000);
    }
  }

  _tile(n, label, { href, sub, tone } = {}) {
    const body = html`<div class="n">${n}</div><div class="l">${label}</div>${sub ? html`<div class="s">${sub}</div>` : nothing}`;
    return href
      ? html`<a class=${'tile ' + (tone ?? '')} href=${href}>${body}</a>`
      : html`<div class=${'tile ' + (tone ?? '')}>${body}</div>`;
  }

  _statusCard(s) {
    const q = s.exception_queue;
    const sp = s.spend;
    return html`
      <div class="tiles">
        ${this._tile(s.pending, 'pending', { href: '/work', sub: 'owed a run' })}
        ${this._tile(s.exceptions, 'exceptions', { href: '/exceptions', sub: 'waiting on a person', tone: s.exceptions ? 'negative' : '' })}
        ${q?.parked
          ? this._tile(age(q.oldest_age_sec), 'oldest parked', { href: '/exceptions', sub: `objective ${age(q.objective_sec)}`, tone: q.met ? '' : 'negative' })
          : nothing}
        ${this._tile(s.claims?.stopped ? 'Stopped' : 'Running', 'claims', {
          sub: s.claims?.actor ? `by ${s.claims.actor}` : 'never touched', tone: s.claims?.stopped ? 'negative' : 'positive' })}
        ${sp ? this._tile(`${sp.running}/${sp.max_concurrent}`, 'running now', { href: '/sessions', sub: sp.started ? `${sp.started} started this window` : '' }) : nothing}
        ${sp ? this._tile(fmtTokens(sp.tokens), 'tokens', { sub: sp.max_tokens ? `of ${fmtTokens(sp.max_tokens)} this window` : 'this window' }) : nothing}
        ${sp && (sp.cost_micros || sp.max_cost_micros)
          ? this._tile(fmtUSD(sp.cost_micros), 'spend', { sub: sp.max_cost_micros ? `of ${fmtUSD(sp.max_cost_micros)}` : 'this window' })
          : nothing}
      </div>
      <dl class="facts">
        <dt>Queue</dt>
        <dd>${s.backlog?.total ? html`${breakdown(s.backlog.by_state)} · <a href="/work">list them</a>` : 'empty'}</dd>
        ${sp && tokenSplit(sp) ? html`<dt>Tokens</dt><dd>${tokenSplit(sp)}</dd>` : nothing}
        <dt>Routes</dt>
        <dd>${s.routes?.length
          ? html`<jh-tag-group>${s.routes.map((r) => html`<jh-tag size="small" label=${r} href="/submit"></jh-tag>`)}</jh-tag-group>`
          : html`<span class="muted">none configured — nothing can be submitted</span>`}</dd>
        <dt>Daemon</dt>
        <dd>${s.runner} ${s.version}, up ${age(s.uptime_sec)}, serving tenant <b>${s.tenant}</b> box <b>${s.box}</b></dd>
      </dl>
      ${/* The alerts sit in the flow, below the numbers they qualify. A
           banner breaks out of the flow and covered the navigation. */ ''}
      ${sp?.unmetered ? html`
        <jh-notification appearance="negative" type="alert" hide-dismiss-button>
          ${sp.unmetered} run(s) this window did not report a cost — the spend above is incomplete and claims are stopped until the window turns.
        </jh-notification>` : nothing}
      ${sp?.unpriced ? html`
        <jh-notification appearance="negative" type="alert" hide-dismiss-button>
          ${sp.unpriced} run(s) this window reported tokens but no cost — the money figure above is incomplete and claims are stopped until the window turns.
        </jh-notification>` : nothing}
      ${sp?.refusing && !sp.unmetered && !sp.unpriced ? html`
        <jh-notification appearance="neutral" type="alert" hide-dismiss-button>
          The meter is pacing work: the factory is idle because it is over budget, not because there is nothing to do.
        </jh-notification>` : nothing}
      ${q && !q.met ? html`
        <jh-notification appearance="negative" type="alert" hide-dismiss-button>
          <span>${q.overdue} exception(s) have waited longer than the ${age(q.objective_sec)} response-time objective — the oldest for ${age(q.oldest_age_sec)}. <a href="/exceptions">Work the queue</a></span>
        </jh-notification>` : nothing}
      <div class="foot">
        <jh-button appearance="tertiary" size="small" label="Reload configuration" @click=${this._reload}>
          <span slot="jh-button-icon-left">${icon('rotate', 'x-small')}</span>
        </jh-button>
        <span class="helper">Re-reads box.yaml on the running daemon. Routes, executors and budget swap hot; tenant, store and socket are identity and are refused.</span>
      </div>`;
  }

  _inFlight(items) {
    const live = (items ?? []).filter((e) => FILTERS.live.includes(e.state));
    if (live.length === 0) return html`<p class="empty">Nothing in flight.</p>`;
    return html`
      <jh-list-group accessible-label="In-flight work">
        ${live.map((e) => html`
          <jh-list-item
            primary-text=${title(e) || e.id}
            secondary-text=${e.note ? firstLine(e.note) : origin(e) || place(e)}
            primary-metadata=${e.stage ?? ''}
            secondary-metadata=${e.stage ? `${age(e.in_state_sec)} in stage` : `${age(e.age_sec)} old`}
            show-divider>
            <span slot="jh-list-item-left">${stateTag(e.state)}</span>
            <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
              label="Open" accessible-label=${`Open ${title(e) || e.id}`} href=${'/items/' + e.id}></jh-button>
          </jh-list-item>`)}
      </jh-list-group>`;
  }

  _feed(titles) {
    const evs = this._activity.events.slice().reverse();
    if (evs.length === 0) return html`<p class="empty">Nothing yet.</p>`;
    return html`
      <ol class="feed" aria-label="Activity feed">
        ${evs.map((a) => {
          const item = itemOf(a);
          return html`
            <li>
              ${kindTag(a.kind)}
              <div>
                <div class="what">${describe(a, titles)}${item ? html` · <a href=${'/items/' + item}>open</a>` : nothing}</div>
                <div class="by">${a.actor}${a.scope && !item ? html` · ${a.scope}` : nothing}</div>
              </div>
              <span class="t"><box-when .at=${a.at}></box-when></span>
            </li>`;
        })}
      </ol>`;
  }

  render() {
    const titles = Object.fromEntries((this._items.value ?? []).map((e) => [e.id, e.task]));
    return html`
      <jh-card header-title="Factory status" header-subtitle="The numbers an operator watches, and what each is made of" padding="medium">
        ${this._status.render({ pending: () => loading('status'), error: failed, complete: (s) => this._statusCard(s) })}
      </jh-card>
      <jh-card header-title="In flight" header-subtitle="What the factory is doing right now, unit by unit" padding="medium">
        ${this._items.render({ pending: () => loading('in-flight work'), error: failed, complete: (items) => this._inFlight(items) })}
      </jh-card>
      <jh-card header-title="Activity" header-subtitle="Everything the factory does, as it does it" padding="medium">
        ${this._feed(titles)}
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
 * barrier, so this stays the representation of record.
 */
export class BoxWork extends LitElement {
  static styles = [base, chips, css`
    :host { display: block; }
    .bar { display: flex; align-items: center; gap: var(--jh-space-300); flex-wrap: wrap; margin-bottom: var(--jh-space-300); }
    .wrap { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; min-width: 820px; table-layout: fixed; }
    caption { text-align: left; padding-bottom: var(--jh-space-200); font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    th, td { text-align: left; vertical-align: top; padding: var(--jh-space-200) var(--jh-space-300) var(--jh-space-200) 0;
             border-bottom: 1px solid var(--jh-color-divider-secondary); }
    thead th { white-space: nowrap; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    thead th button { font: inherit; cursor: pointer; padding: 0; border: 0; background: transparent; color: inherit; }
    thead th[aria-sort="ascending"] button::after { content: ' \\2191'; }
    thead th[aria-sort="descending"] button::after { content: ' \\2193'; }
    tbody th { font-weight: 500; }
    tbody th a { text-decoration: none; }
    tbody th a:hover { text-decoration: underline; }
    .note { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
            font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); margin-top: 2px; }
    .stack { display: grid; gap: 4px; justify-items: start; }
    .ready { font: var(--jh-font-micro-medium); color: var(--jh-color-content-positive-enabled); text-transform: uppercase; }
    .lapsed { color: var(--jh-color-content-negative-enabled); font-weight: 600; }
    .nowrap { white-space: nowrap; }
    /* The Work column has no other bound on its content: title() clips the
       common case, but a bare task string with no newline reaches here
       unclipped, so the cell itself must refuse to grow past one line. */
    .worktitle { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  `];
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
        <div class="bar">
          <div class="chips" role="group" aria-label="Show">
            ${Object.keys(FILTERS).map((f) => html`
              <button type="button" aria-pressed=${this._filter === f ? 'true' : 'false'}
                @click=${() => { this._filter = f; }}>${f}</button>`)}
          </div>
        </div>
        ${this._task.render({
          pending: () => loading('work'),
          error: failed,
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
          <colgroup>
            <col style="width: 108px"><col><col style="width: 150px"><col style="width: 110px">
            <col style="width: 160px"><col style="width: 150px"><col style="width: 64px">
          </colgroup>
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
                <td><div class="stack">${stateTag(e.state)}${e.ready ? html`<span class="ready">ready</span>` : nothing}</div></td>
                <th scope="row">
                  <a class="worktitle" href=${'/items/' + e.id}>${title(e) || e.id}</a>
                  ${e.note ? html`<div class="note" title=${e.note}>${firstLine(e.note)}</div>` : nothing}
                </th>
                <td>${e.blocked_by?.length
                  ? e.blocked_by.map((d, i) => html`${i ? ', ' : ''}<a href=${'/items/' + d}>${short(d)}</a>`)
                  : why(e) || html`<span class="muted">—</span>`}</td>
                <td class="nowrap">${e.stage ?? ''}
                  ${e.stage ? html`<div class="helper">${age(e.in_state_sec)}</div>` : nothing}</td>
                <td class=${e.lease_lapsed ? 'lapsed' : ''}>${hold(e) || html`<span class="muted">—</span>`}</td>
                <td class="muted">${origin(e) || place(e)}</td>
                <td class="nowrap">${age(e.age_sec)}</td>
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
    case 'tool': {
      if (!f.tool) return '(unnamed — nothing for policy to match)';
      const d = toolDetail(f);
      return d ? html`<b>${f.tool}</b> ${d}` : f.tool;
    }
    case 'denied': return `${f.tool} — refused by policy at the stream`;
    case 'escalate': return f.reason;
    case 'handoff': return f.reason;
    default: return f.text ?? '';
  }
}

// The frame kinds an operator should notice without looking for them.
const FRAME_APPEARANCE = { denied: 'negative', escalate: 'negative', handoff: 'neutral', started: 'positive' };

/** collapse folds a call said twice into one row. The adapter announces a
 *  Read or a Bash before it knows the path or the command line and titles it
 *  on a later update; the runtime forwards both, under the same call id, and
 *  the timeline should read as one call that now says what it did. Frames
 *  without a call id are kept as they are. Oldest first in, oldest first out. */
export function collapse(frames) {
  const out = [];
  const at = new Map();
  for (const f of frames) {
    const key = f.kind === 'tool' && f.call ? f.call : '';
    if (key && at.has(key)) {
      const i = at.get(key);
      out[i] = { ...out[i], text: f.text || out[i].text };
      continue;
    }
    if (key) at.set(key, out.length);
    out.push(f);
  }
  return out;
}

/** frameKey names a frame the same way whether it came off the live stream
 *  or out of the run log: the daemon stamps one moment on both. */
const frameKey = (f) => `${f.at}|${f.kind}|${f.call ?? ''}|${f.tool ?? ''}`;

/**
 * Sessions: one card per running unit — what it is, how far it has got, and
 * what its agent is doing right now.
 *
 * Cards side by side rather than one merged feed, because fan-out is the case
 * a feed breaks on: several runs interleave into a stream that reads as noise,
 * and the question an operator has — is THIS unit moving — is then answered by
 * scrolling.
 */
export class BoxSessions extends LitElement {
  static styles = [base, css`
    :host { display: block; }
    .grid { display: grid; gap: var(--jh-space-400); grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); align-items: start; }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); margin-bottom: var(--jh-space-200); }
    .layers { display: flex; gap: var(--jh-space-100); flex-wrap: wrap; margin-bottom: var(--jh-space-200); }
    .layer { font: var(--jh-font-micro-medium); padding: 2px var(--jh-space-200); border-radius: var(--jh-border-radius-100);
             background: var(--jh-color-container-secondary-enabled); }
    h4 { margin: var(--jh-space-300) 0 var(--jh-space-100); font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    ol { list-style: none; margin: 0; padding: 0; max-height: 480px; overflow-y: auto; font: var(--jh-font-helper-regular); }
    li { display: grid; grid-template-columns: 64px 64px 1fr; gap: var(--jh-space-200); padding: 3px 0;
         border-bottom: 1px solid var(--jh-color-divider-secondary); }
    li .x b { font-weight: 600; }
    li .t { color: var(--jh-color-content-secondary-enabled); font-variant-numeric: tabular-nums; }
    li .k { font-weight: 600; }
    li .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    li.negative .k { color: var(--jh-color-content-negative-enabled); }
    li.positive .k { color: var(--jh-color-content-positive-enabled); }
    .blind { color: var(--jh-color-content-secondary-enabled); font-style: italic; margin: 0; }
  `];
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
    return collapse([...(s.timeline ?? []), ...(this._frames[s.id] ?? []).filter((f) => !seen.has(f.seq))]);
  }

  render() {
    return this._task.render({
      pending: () => html`<jh-card header-title="Sessions" padding="medium">${loading('sessions')}</jh-card>`,
      error: (e) => html`<jh-card header-title="Sessions" padding="medium">${failed(e)}</jh-card>`,
      complete: (sessions) => (sessions?.length ?? 0) === 0
        ? html`<jh-card header-title="Sessions" header-subtitle="Each running unit, and what its agent is doing right now" padding="medium">
            <p class="empty">No unit is running. Claimed work appears here as it starts.</p>
          </jh-card>`
        : html`<div class="grid">${sessions.map((s) => this._card(s))}</div>`,
    });
  }

  _card(s) {
    const frames = this._timeline(s);
    return html`
      <jh-card header-title=${title(s) || s.id} header-subtitle=${origin(s) || place(s)} padding="medium">
        <div class="meta">
          ${s.judgement ? html`<jh-tag size="small" label="judgement"></jh-tag>` : stateTag(s.state)}
          ${s.stage ? html`<jh-tag size="small" label=${s.stage}></jh-tag><span>${age(s.in_state_sec)} in stage</span>` : nothing}
          ${s.seat ? html`<span>seat <b>${s.seat}</b></span>` : nothing}
          ${s.runner ? html`<span>${hold(s)}</span>` : nothing}
        </div>
        <div class="meta">
          ${s.judgement
            ? html`<span>ingress <box-hash label="ingress id" .value=${s.id} href=${'/ingress/' + s.id} chars="18"></box-hash></span>`
            : html`<span>item <box-hash label="item id" .value=${s.id} href=${'/items/' + s.id} chars="18"></box-hash></span>`}
          ${s.agentref ? html`<span>agent <box-hash label="agentref" .value=${s.agentref}></box-hash></span>` : nothing}
          ${s.head ? html`<span>head <box-hash label="commit" .value=${s.head} href=${'/provenance/' + s.head}></box-hash></span>` : nothing}
        </div>
        ${/* Which layers, in which order — the agentref is a digest and alone
             it answers nothing about what the agent actually was. */ ''}
        ${s.layers?.length
          ? html`<div class="layers" aria-label="Agent layers">${s.layers.map((l) => html`
              <span class="layer" title=${`${l.source} ${l.digest}`}>${l.scope}:${l.name}</span>`)}</div>`
          : nothing}
        <h4>Timeline <span class="helper">— newest first</span></h4>
        ${/* Newest at the top: the question is what the agent is doing NOW,
             and history is what you scroll down into. */ ''}
        ${frames.length === 0
          ? (s.watching
              ? html`<p class="empty">Started; nothing said yet.</p>`
              : html`<p class="blind">No live stream here — another runner holds this, or the window has rolled.
                     Silence about the run, not an idle agent.</p>`)
          : html`<ol aria-label="Agent timeline, newest first">${frames.slice().reverse().map((f) => html`
              <li class=${FRAME_APPEARANCE[f.kind] ?? ''}>
                <span class="t"><box-when .at=${f.at}></box-when></span>
                <span class="k">${f.kind}</span>
                <span class="x">${frameLine(f)}</span>
              </li>`)}
            </ol>`}
      </jh-card>`;
  }
}
customElements.define('box-sessions', BoxSessions);

/**
 * Exceptions: what still needs a person, and the two ways off the queue.
 *
 * Loudest first, then oldest — the order a person should work the queue.
 * Each card is one decision: retry it, or resolve it with a reason. Those
 * verbs were on the API from the day the queue existed and on no page; the
 * room could show a person their work and not let them do it.
 */
export class BoxExceptions extends LitElement {
  static styles = [base, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); margin-bottom: var(--jh-space-300); }
    .overdue { color: var(--jh-color-content-negative-enabled); font-weight: 600; }
    .note { margin: 0 0 var(--jh-space-300); padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
            background: var(--jh-color-container-secondary-enabled); white-space: pre-wrap; overflow-wrap: anywhere; }
    .summary { display: flex; gap: var(--jh-space-400); flex-wrap: wrap; align-items: center; }
  `];

  _task = new Task(this, { task: async (_, { signal }) => getExceptions(signal), args: () => [] });

  constructor() {
    super();
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (/index\/exceptions|workitem\//.test(a.scope)) this._refresh(); },
    });
  }

  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => this._task.run(), 400);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
  }

  _done(verb, it) {
    toast(`${verb}: ${title(it) || it.id}`, 'positive');
    this._refresh();
  }

  render() {
    return this._task.render({
      pending: () => html`<jh-card header-title="Exception queue" padding="medium">${loading('exceptions')}</jh-card>`,
      error: (e) => html`<jh-card header-title="Exception queue" padding="medium">${failed(e)}</jh-card>`,
      complete: (items) => html`
        <jh-card header-title="Exception queue" header-subtitle="Parked work waiting on a person, loudest and oldest first" padding="medium">
          ${(items?.length ?? 0) === 0
            ? html`<p class="empty">Nothing here — the factory has nothing waiting on a person.</p>`
            : html`<div class="summary">
                <span><b>${items.length}</b> parked</span>
                <span class=${items.some((i) => i.overdue) ? 'overdue' : ''}><b>${items.filter((i) => i.overdue).length}</b> past the response-time objective</span>
                <span class="helper">Retry hands the unit back to the factory. Resolve closes it with your reason on the record.</span>
              </div>`}
        </jh-card>
        ${(items ?? []).map((it) => this._card(it))}`,
    });
  }

  _card(it) {
    return html`
      <jh-card header-title=${title(it) || it.id} header-subtitle=${origin(it) || place(it)} padding="medium">
        <div class="meta">
          <jh-tag size="small" class=${SEVERITY_APPEARANCE[it.severity ?? 'medium'] ?? 'neutral'} label=${`severity ${it.severity ?? 'medium'}`}></jh-tag>
          ${it.stage ? html`<jh-tag size="small" label=${`at ${it.stage}`}></jh-tag>` : nothing}
          <span>parked <box-when .at=${it.parked}></box-when></span>
          ${it.overdue ? html`<span class="overdue">past the response-time objective</span>` : nothing}
          ${it.escalations ? html`<span>escalated out of band ${it.escalations}×</span>` : nothing}
          <span>item <box-hash label="item id" .value=${it.id} href=${'/items/' + it.id} chars="18"></box-hash></span>
        </div>
        ${it.note ? html`<p class="note">${it.note}</p>` : nothing}
        <div class="actions">
          <box-reason label="Retry" confirm="Retry" appearance="primary" glyph="rotate" prompt=${`Retry ${title(it)}`}
            .action=${(r) => retryItem(it.id, r)} @box-done=${() => this._done('Retried', it)}></box-reason>
          <box-reason label="Resolve" confirm="Resolve" appearance="secondary" glyph="check" prompt=${`Resolve ${title(it)}`}
            .action=${(r) => resolveItem(it.id, r)} @box-done=${() => this._done('Resolved', it)}></box-reason>
          <jh-button appearance="tertiary" size="small" label="Open" accessible-label=${`Open ${title(it) || it.id}`} href=${'/items/' + it.id}></jh-button>
        </div>
      </jh-card>`;
  }
}
customElements.define('box-exceptions', BoxExceptions);

/** One item: what it is, where it stands, what can be done, and its whole chain. */
export class BoxItem extends LitElement {
  static properties = { itemId: { attribute: false }, _live: { state: true } };
  static styles = [base, facts, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; margin-bottom: var(--jh-space-300); }
    .note { margin: var(--jh-space-300) 0 0; white-space: pre-wrap; overflow-wrap: anywhere; }
    details { margin-top: var(--jh-space-300); }
    summary { cursor: pointer; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    pre { white-space: pre-wrap; overflow-wrap: anywhere; font: var(--jh-font-code-regular-2); margin: var(--jh-space-200) 0 0;
          padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200); background: var(--jh-color-container-secondary-enabled); }
    .actions { margin-top: var(--jh-space-400); }
    .lapsed { color: var(--jh-color-content-negative-enabled); font-weight: 600; }
    ol.chain { list-style: none; margin: 0; padding: 0; }
    ol.chain li { display: grid; grid-template-columns: minmax(92px, max-content) 1fr max-content; gap: var(--jh-space-300);
                  align-items: baseline; padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.chain .what { overflow-wrap: anywhere; }
    ol.chain .by { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    ol.chain .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    ol.frames { list-style: none; margin: 0; padding: 0; }
    ol.frames li { display: grid; grid-template-columns: 5.5em 6em 1fr; gap: var(--jh-space-300); align-items: baseline;
                   padding: var(--jh-space-100) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.frames .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    ol.frames .k { font: var(--jh-font-helper-medium); }
    ol.frames .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    ol.frames li.negative .k { color: var(--jh-color-content-negative-enabled); }
    ol.frames li.positive .k { color: var(--jh-color-content-positive-enabled); }
  `];
  constructor() {
    super();
    this._live = [];
    // A run's own records — the agent starting, each gate verdict, an
    // escalation and its reason — reach the feed, so this page follows a
    // unit as it works instead of being a snapshot you reload. A terminal
    // record also refetches the run log, so the frames that streamed are
    // replaced by the frames that were kept.
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (a.scope === `workitem/${this.itemId}` || a.subject === this.itemId) this._refresh(); },
    });
  }

  // The item's own stream: what its agent is doing, as it does it, with the
  // run so far replayed on connect. Scoped to the item server-side and
  // re-opened when the page moves to another item.
  willUpdate(changed) {
    if (changed.has('itemId')) {
      if (this._stream) { this.removeController(this._stream); this._stream.hostDisconnected(); }
      this._live = [];
      this._stream = new ActivityController(this, {
        item: this.itemId, limit: 1,
        onSession: (f) => { this._live = [...this._live, f]; },
      });
    }
  }

  // What the run did: the kept frames first, then whatever has streamed
  // since that is not already among them. The daemon stamps one moment on a
  // frame for both, so the same frame seen twice is recognised as one.
  _frames(kept) {
    const seen = new Set((kept ?? []).map(frameKey));
    return collapse([...(kept ?? []), ...this._live.filter((f) => !seen.has(frameKey(f)))]);
  }

  _agent(item, kept) {
    const frames = this._frames(kept);
    const running = item.state === 'claimed';
    const subtitle = running
      ? 'What the agent is doing, as it does it — newest first'
      : 'What the agent did, from the run log — newest first';
    return html`
      <jh-card header-title="Agent" header-subtitle=${subtitle} padding="medium">
        ${frames.length === 0
          ? html`<p class="empty">${running
              ? 'Nothing streamed yet. The first frame is the agent starting.'
              : 'Nothing was kept for this run — it ran before the run log existed, or its frames were pruned.'}</p>`
          : html`<ol class="frames" aria-label="Agent timeline, newest first">${frames.slice().reverse().map((f) => html`
              <li class=${FRAME_APPEARANCE[f.kind] ?? ''}>
                <span class="t"><box-when .at=${f.at}></box-when></span>
                <span class="k">${f.kind}</span>
                <span class="x">${frameLine(f)}</span>
              </li>`)}</ol>`}
      </jh-card>`;
  }

  // Three reads, one page. The raw work item carries what the queue wrote —
  // dependencies, the prior attempt, the requester — and none of what the
  // room derives: when it opened, how long it has sat, which seat holds it,
  // the lease. The room's entry has those and the chain has everything.
  _task = new Task(this, {
    task: async ([id], { signal }) => {
      const [item, entry, evidence, kept] = await Promise.all([
        getItem(id, signal),
        getSession(id, signal).catch(() => ({})),
        getItemEvidence(id, signal),
        getRunFrames(id, signal).catch(() => []),
      ]);
      return { item: { ...entry, ...item, note: item.note ?? entry.note }, evidence, kept };
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

  _done(verb, item) {
    toast(`${verb}: ${title(item) || item.id}`, 'positive');
    this._refresh();
  }

  _head(item) {
    const link = taskURL(item.task);
    const parkedOrFailed = item.state === 'parked' || item.state === 'failed';
    const tone = parkedOrFailed ? 'negative' : item.state === 'done' ? 'positive' : 'neutral';
    return html`
      <jh-card header-title=${title(item) || item.id} header-subtitle=${origin(item) || place(item)} padding="medium">
        <div class="meta">
          ${stateTag(item.state)}
          ${item.stage && item.stage !== item.state ? html`<jh-tag size="small" label=${`at ${item.stage}`}></jh-tag>` : nothing}
          ${item.severity ? html`<jh-tag size="small" class=${SEVERITY_APPEARANCE[item.severity] ?? 'neutral'} label=${`severity ${item.severity}`}></jh-tag>` : nothing}
          ${link ? html`<jh-button appearance="tertiary" size="small" label=${/github\.com/.test(link) ? 'Open on GitHub' : 'Open link'} href=${link} target="_blank">
              <span slot="jh-button-icon-right">${icon('external', 'x-small')}</span>
            </jh-button>` : nothing}
        </div>
        ${item.note ? html`
          <jh-notification appearance=${tone} type="alert" hide-dismiss-button>
            <div class="note">${item.note}</div>
          </jh-notification>` : nothing}
        <dl class="facts" style="margin-top: var(--jh-space-400)">
          <dt>Item</dt><dd><box-hash label="item id" .value=${item.id} chars="24"></box-hash></dd>
          <dt>Where</dt><dd>${place(item) || html`<span class="muted">—</span>`}${item.requester ? html` · requested by ${item.requester}` : nothing}</dd>
          <dt>Ingress</dt><dd><box-hash label="ingress id" .value=${item.ingress} chars="24"></box-hash></dd>
          ${item.head ? html`<dt>Head</dt><dd><box-hash label="commit" .value=${item.head} href=${'/provenance/' + item.head}></box-hash> <span class="helper">landed commit — its provenance</span></dd>` : nothing}
          ${item.built && item.built !== item.head ? html`<dt>Built</dt><dd><box-hash label="commit" .value=${item.built} href=${'/provenance/' + item.built}></box-hash></dd>` : nothing}
          ${item.seat ? html`<dt>Seat</dt><dd>${item.seat}${item.agentref ? html` · agent <box-hash label="agentref" .value=${item.agentref}></box-hash>` : nothing}</dd>` : nothing}
          ${item.runner ? html`<dt>Held by</dt><dd class=${item.lease_lapsed ? 'lapsed' : ''}>${hold(item)}</dd>` : nothing}
          ${item.blocked_by?.length ? html`<dt>Blocked by</dt><dd>${item.blocked_by.map((d, i) => html`${i ? ', ' : ''}<a href=${'/items/' + d}>${d}</a>`)}</dd>` : nothing}
          ${item.root ? html`<dt>Cascaded from</dt><dd><a href=${'/items/' + item.root}>${item.root}</a></dd>` : nothing}
          ${item.depends_on?.length ? html`<dt>Depends on</dt><dd>${item.depends_on.map((d, i) => html`${i ? ', ' : ''}<a href=${'/items/' + d}>${d}</a>`)}</dd>` : nothing}
          ${item.prior ? html`<dt>Prior attempt</dt><dd>carried into this run</dd>` : nothing}
          ${item.opened ? html`<dt>Opened</dt><dd><box-when .at=${item.opened} abs></box-when> <span class="helper">(<box-when .at=${item.opened}></box-when>)</span></dd>` : nothing}
          ${item.changed ? html`<dt>Last change</dt><dd><box-when .at=${item.changed} abs></box-when> <span class="helper">(${age(item.in_state_sec)} in this state)</span></dd>` : nothing}
          ${item.hops ? html`<dt>Sessions</dt><dd>${item.hops + 1}, after ${item.hops} handoff(s)</dd>` : nothing}
        </dl>
        <details>
          <summary>Full task text</summary>
          <pre>${item.task}</pre>
        </details>
        <div class="actions">
          ${parkedOrFailed ? html`
            <box-reason label="Retry" confirm="Retry" appearance="primary" glyph="rotate" prompt="Retry this item"
              .action=${(r) => retryItem(item.id, r)} @box-done=${() => this._done('Retried', item)}></box-reason>
            <box-reason label="Resolve" confirm="Resolve" appearance="secondary" glyph="check" prompt="Resolve this item"
              .action=${(r) => resolveItem(item.id, r)} @box-done=${() => this._done('Resolved', item)}></box-reason>
            <span class="helper">Retry hands the unit back to the factory; resolve closes it with your reason on the record.</span>`
            : item.state === 'claimed'
              ? html`<span class="helper">A running unit can only be stopped with the rest of the factory — the cord is at the top of the page.</span>`
              : html`<span class="helper">Retry and resolve apply to parked or failed work.</span>`}
        </div>
      </jh-card>`;
  }

  _chain(evidence) {
    if ((evidence?.length ?? 0) === 0) return html`<p class="empty">Nothing recorded yet.</p>`;
    return html`
      <ol class="chain" aria-label="Run timeline">
        ${evidence.map((r) => html`
          <li>
            ${kindTag(r.Kind)}
            <div>
              <div class="what">${describe(r)}</div>
              <div class="by">${r.Actor}${r.Subject && r.Subject !== this.itemId ? html` · ${short(r.Subject)}` : nothing}</div>
            </div>
            <span class="t"><box-when .at=${r.At}></box-when></span>
          </li>`)}
      </ol>`;
  }

  render() {
    return this._task.render({
      pending: () => loading('item'),
      error: failed,
      complete: ({ item, evidence, kept }) => html`
        ${this._head(item)}
        ${this._agent(item, kept)}
        <jh-card header-title="Timeline" header-subtitle="What this unit did, in order, from its own evidence chain" padding="medium">
          ${this._chain(evidence)}
        </jh-card>`,
    });
  }
}
customElements.define('box-item', BoxItem);

/**
 * One judgement's own page: the origination's facts and the same timeline a
 * unit's item page shows. Not as rich as box-item on purpose — a judgement is
 * the planner's own act, with no queue row, no gates, no chain of verbs of
 * its own — only the origination record and the run log #44 already
 * produces under "plan-<ingress-id>".
 */
export class BoxIngress extends LitElement {
  static properties = { ingressId: { attribute: false }, _live: { state: true } };
  static styles = [base, facts, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; margin-bottom: var(--jh-space-300); }
    details { margin-top: var(--jh-space-300); }
    summary { cursor: pointer; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    pre { white-space: pre-wrap; overflow-wrap: anywhere; font: var(--jh-font-code-regular-2); margin: var(--jh-space-200) 0 0;
          padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200); background: var(--jh-color-container-secondary-enabled); }
    ol.frames { list-style: none; margin: 0; padding: 0; }
    ol.frames li { display: grid; grid-template-columns: 5.5em 6em 1fr; gap: var(--jh-space-300); align-items: baseline;
                   padding: var(--jh-space-100) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.frames .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    ol.frames .k { font: var(--jh-font-helper-medium); }
    ol.frames .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    ol.frames li.negative .k { color: var(--jh-color-content-negative-enabled); }
    ol.frames li.positive .k { color: var(--jh-color-content-positive-enabled); }
  `];

  constructor() {
    super();
    this._live = [];
  }

  // The judgement's own stream, scoped server-side to "plan-<id>" exactly as
  // #44 keys it — reopened when the page moves to another origination,
  // following box-item's willUpdate wiring precisely.
  willUpdate(changed) {
    if (changed.has('ingressId')) {
      if (this._stream) { this.removeController(this._stream); this._stream.hostDisconnected(); }
      this._live = [];
      this._stream = new ActivityController(this, {
        item: 'plan-' + this.ingressId, limit: 1,
        onSession: (f) => { this._live = [...this._live, f]; },
      });
    }
  }

  _task = new Task(this, {
    task: async ([id], { signal }) => {
      const [ing, kept] = await Promise.all([
        getIngress(id, signal),
        getRunFrames('plan-' + id, signal).catch(() => []),
      ]);
      return { ing, kept };
    },
    args: () => [this.ingressId],
  });

  // The kept frames first, then whatever has streamed since that is not
  // already among them — the same merge-dedup box-item uses, on the same
  // frameKey the daemon stamps on both.
  _frames(kept) {
    const seen = new Set((kept ?? []).map(frameKey));
    return collapse([...(kept ?? []), ...this._live.filter((f) => !seen.has(frameKey(f)))]);
  }

  _agent(ing, kept) {
    const frames = this._frames(kept);
    const running = !ing.standing;
    const subtitle = running
      ? 'What the agent is doing, as it does it — newest first'
      : 'What the agent did, from the run log — newest first';
    return html`
      <jh-card header-title="Agent" header-subtitle=${subtitle} padding="medium">
        ${frames.length === 0
          ? html`<p class="empty">${running
              ? 'Nothing streamed yet. The first frame is the agent starting.'
              : 'Nothing was kept for this run — it ran before the run log existed, or its frames were pruned.'}</p>`
          : html`<ol class="frames" aria-label="Agent timeline, newest first">${frames.slice().reverse().map((f) => html`
              <li class=${FRAME_APPEARANCE[f.kind] ?? ''}>
                <span class="t"><box-when .at=${f.at}></box-when></span>
                <span class="k">${f.kind}</span>
                <span class="x">${frameLine(f)}</span>
              </li>`)}</ol>`}
      </jh-card>`;
  }

  _head(ing) {
    const running = !ing.standing;
    return html`
      <jh-card header-title=${title(ing) || ing.id} header-subtitle=${place(ing)} padding="medium">
        <div class="meta">
          <jh-tag size="small" class=${running ? 'brand' : (STANDING_APPEARANCE[ing.standing] ?? 'neutral')} label=${running ? 'judgement' : ing.standing}></jh-tag>
        </div>
        <dl class="facts" style="margin-top: var(--jh-space-400)">
          <dt>Ingress</dt><dd><box-hash label="ingress id" .value=${ing.id} chars="24"></box-hash></dd>
          <dt>Where</dt><dd>${place(ing) || html`<span class="muted">—</span>`}${ing.requester ? html` · requested by ${ing.requester}` : nothing}</dd>
          ${ing.opened ? html`<dt>Opened</dt><dd><box-when .at=${ing.opened} abs></box-when> <span class="helper">(<box-when .at=${ing.opened}></box-when>)</span></dd>` : nothing}
          ${ing.reason ? html`<dt>Reason</dt><dd>${ing.reason}</dd>` : nothing}
        </dl>
        <details>
          <summary>Full task text</summary>
          <pre>${ing.task}</pre>
        </details>
      </jh-card>`;
  }

  render() {
    return this._task.render({
      pending: () => loading('judgement'),
      error: failed,
      complete: ({ ing, kept }) => html`
        ${this._head(ing)}
        ${this._agent(ing, kept)}`,
    });
  }
}
customElements.define('box-ingress', BoxIngress);

/** Provenance: the audit query — the chain one commit is a link of, every
 *  span verified, every gap named. The ordered list is the representation of
 *  record; the diagram arrives from the server already laid out (the browser
 *  renders, it does not lay out) and sits on top, hidden from screen
 *  readers. The export link is the same chain as one self-contained file. */
export class BoxProvenance extends LitElement {
  static properties = { sha: { attribute: false } };
  static styles = [base, css`
    :host { display: block; }
    .sha { margin: 0 0 var(--jh-space-300); }
    .diagram { overflow-x: auto; margin: var(--jh-space-300) 0; color: var(--jh-color-content-primary-enabled); }
    .gap { margin-bottom: var(--jh-space-300); }
    .span { margin: var(--jh-space-400) 0 0; }
    .span h3 { margin: 0 0 var(--jh-space-100); font: var(--jh-font-body-md-semibold); }
    .verify { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .verify.broken { color: var(--jh-color-content-danger-enabled); font-weight: 600; }
    .worm { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    ol { list-style: none; margin: 0; padding: 0; }
    li { display: grid; grid-template-columns: minmax(92px, max-content) 1fr max-content; gap: var(--jh-space-300);
         align-items: baseline; padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    li.context { opacity: 0.65; }
    .by { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    .actions { margin-top: var(--jh-space-400); }
  `];
  _task = new Task(this, {
    task: async ([sha], { signal }) => (sha ? getProvenanceChain(sha, signal) : null),
    args: () => [this.sha],
  });

  // The whole span, exactly as the export shows it: a chain proves nothing
  // from the middle, so the surrounding records render too, muted — the
  // links naming the commit carry the weight.
  _span(span) {
    const named = new Set(span.names ?? []);
    return html`
      <section class="span">
        <h3>${span.scope}</h3>
        ${span.verify
          ? html`<p class="verify broken">Chain broken: ${span.verify}</p>`
          : span.whole
            ? html`<p class="verify">${span.total} record(s) — chain verifies end to end.</p>`
            : html`<p class="verify">Standing scope — ${span.total} record(s) verify; showing the ${span.records?.length ?? 0} naming this commit.</p>`}
        ${span.compacted ? html`<p class="worm">Crosses into WORM: records ${span.compacted.from}–${span.compacted.through} at ${span.compacted.export?.uri}${span.compacted.export?.retain_until && span.compacted.export.retain_until !== '0001-01-01T00:00:00Z' ? html`, retention locked until <box-when .at=${span.compacted.export.retain_until} abs></box-when>` : ', no retention lock (local sink)'}</p>` : nothing}
        <ol aria-label=${`Records in ${span.scope}`}>
          ${(span.records ?? []).map((r) => {
            const item = itemOf(r);
            return html`
              <li class=${named.has(r.Seq) ? '' : 'context'}>
                ${kindTag(r.Kind)}
                <div>
                  <div>${describe(r)}${item ? html` · <a href=${'/items/' + item}>open the item</a>` : nothing}</div>
                  <div class="by">${r.Actor}${r.OnBehalfOf ? ` for ${r.OnBehalfOf}` : ''} · ${r.Scope} #${r.Seq}${named.has(r.Seq) ? ' · names this commit' : ''}</div>
                </div>
                <span class="t"><box-when .at=${r.At}></box-when></span>
              </li>`;
          })}
        </ol>
      </section>`;
  }

  render() {
    return html`
      <jh-card header-title="Provenance" header-subtitle="The chain this commit is a link of — every span verified, every gap named" padding="medium">
        <p class="sha">Commit <box-hash label="commit" .value=${this.sha} chars="16"></box-hash></p>
        ${this._task.render({
          pending: () => loading('provenance'),
          error: failed,
          complete: (view) => {
            const chain = view?.chain ?? {};
            if ((chain.spans?.length ?? 0) === 0) {
              return html`<p class="empty">Nothing is recorded about this commit. Either it did not come through this factory, or it has not landed yet.</p>`;
            }
            return html`
              ${(chain.gaps ?? []).map((g) => html`
                <jh-notification class="gap" appearance="negative" type="alert" hide-dismiss-button>
                  <strong>Missing ${g.link}</strong> — ${g.why}
                </jh-notification>`)}
              ${view.svg ? html`<div class="diagram" aria-hidden="true">${unsafeHTML(view.svg)}</div>` : nothing}
              ${chain.spans.map((s) => this._span(s))}
              <div class="actions">
                <jh-button appearance="secondary" size="small" label="Evidence artifact"
                  href=${provenanceExportURL(this.sha)} target="_blank"></jh-button>
                <span class="helper">One self-contained file: the same chain with its full records, verifiable offline — save it to hand an auditor complete evidence.</span>
              </div>`;
          },
        })}
      </jh-card>`;
  }
}
customElements.define('box-provenance', BoxProvenance);

/** Plan: the decomposition graph for one origination, rendered before
 *  anything claims a node. The ordered node list is the representation of
 *  record — it is also the form that pastes into an approval record, so it
 *  has to be complete and readable with the diagram gone — and the diagram
 *  arrives from the server already laid out, aria-hidden underneath it. */
export class BoxPlan extends LitElement {
  static properties = { ref: { attribute: false } };
  static styles = [base, facts, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .fanout { font: var(--jh-font-heading-medium-4); margin: 0 0 var(--jh-space-300); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; margin-bottom: var(--jh-space-300); }
    .diagram { overflow-x: auto; margin: var(--jh-space-300) 0; color: var(--jh-color-content-primary-enabled); }
    table { border-collapse: collapse; width: 100%; margin-bottom: var(--jh-space-300); }
    caption { text-align: left; padding-bottom: var(--jh-space-200); font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    th, td { text-align: left; vertical-align: top; padding: var(--jh-space-200) var(--jh-space-300) var(--jh-space-200) 0;
             border-bottom: 1px solid var(--jh-color-divider-secondary); }
    thead th { white-space: nowrap; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    ol.nodes { list-style: none; margin: 0; padding: 0; }
    ol.nodes li { padding: var(--jh-space-300) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.nodes .nid { font: var(--jh-font-body-md-semibold); }
    ol.nodes .ntask { margin: var(--jh-space-100) 0; white-space: pre-wrap; overflow-wrap: anywhere; }
    .diffs { display: grid; gap: var(--jh-space-400); grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
    .diffs h3 { margin: 0 0 var(--jh-space-100); font: var(--jh-font-body-md-semibold); }
    .diffs ol { list-style: none; margin: 0; padding: 0; }
    .diffs li { padding: var(--jh-space-100) 0; overflow-wrap: anywhere; border-bottom: 1px solid var(--jh-color-divider-secondary); }
  `];

  _task = new Task(this, {
    task: async ([ref], { signal }) => {
      const plan = await getPlan(ref, signal);
      const list = await getIngressPlans(plan.ingress, signal);
      const idx = list.findIndex((p) => p.ref === plan.ref);
      const previous = idx > 0 ? list[idx - 1] : null;
      const diff = previous ? await getPlanDiff(previous.ref, plan.ref, signal) : null;
      return { plan, previous, diff };
    },
    args: () => [this.ref],
  });

  _node(n, i) {
    return html`
      <li>
        <div class="nid">${i + 1}. <code>${n.id}</code></div>
        <div class="ntask">${n.task}</div>
        <dl class="facts">
          <dt>Repo</dt><dd>${n.repo || html`<span class="muted">inherits the origination's</span>`}</dd>
          <dt>Depends on</dt><dd>${n.depends_on?.length ? n.depends_on.join(', ') : html`<span class="muted">nothing</span>`}</dd>
          <dt>Sections</dt><dd>${n.sections?.length ? n.sections.join(', ') : html`<span class="muted">whole repository</span>`}</dd>
          <dt>Model</dt><dd>${n.model || html`<span class="muted">default</span>`}</dd>
        </dl>
      </li>`;
  }

  _diffList(label, nodes, describeNode) {
    return html`
      <div>
        <h3>${label} (${nodes?.length ?? 0})</h3>
        ${nodes?.length
          ? html`<ol aria-label=${`${label} nodes`}>${nodes.map((n) => html`<li>${describeNode(n)}</li>`)}</ol>`
          : html`<p class="empty">None.</p>`}
      </div>`;
  }

  _diffSection(previous, diff) {
    if (!previous) {
      return html`
        <jh-card header-title="Change from the previous plan" padding="medium">
          <p class="empty">First plan recorded for this origination — nothing to diff against.</p>
        </jh-card>`;
    }
    return html`
      <jh-card header-title="Change from the previous plan" header-subtitle=${`Against ${previous.ref}`} padding="medium">
        <div class="diffs">
          ${this._diffList('Added', diff.Added, (n) => html`<code>${n.id}</code> — ${n.task}`)}
          ${this._diffList('Removed', diff.Removed, (n) => html`<code>${n.id}</code> — ${n.task}`)}
          ${this._diffList('Changed', diff.Changed, (c) => html`<code>${c.ID}</code> — ${c.Before.task} → ${c.After.task}`)}
        </div>
      </jh-card>`;
  }

  render() {
    return this._task.render({
      pending: () => loading('plan'),
      error: failed,
      complete: ({ plan, previous, diff }) => html`
        <jh-card header-title="Plan" header-subtitle=${plan.ref} padding="medium">
          <p class="fanout">${plan.fan_out} unit(s)</p>
          <div class="meta">
            <jh-tag size="small" label=${plan.decomposer}></jh-tag>
            <jh-tag size="small" class=${plan.replayable ? 'positive' : 'neutral'} label=${plan.replayable ? 'replayable' : 'judgement'}></jh-tag>
          </div>
          ${plan.replayable
            ? html`
              <table>
                <caption>Facts read, and when</caption>
                <thead><tr><th scope="col">Key</th><th scope="col">Value</th><th scope="col">As of</th></tr></thead>
                <tbody>
                  ${(plan.facts ?? []).map((f) => html`
                    <tr><td>${f.key}</td><td>${f.value}</td><td><box-when .at=${f.as_of} abs></box-when></td></tr>`)}
                </tbody>
              </table>`
            : html`<p>Produced by agent <box-hash label="agentref" .value=${plan.agentref}></box-hash></p>`}
          <ol class="nodes" aria-label="Plan nodes, in order">
            ${plan.nodes.map((n, i) => this._node(n, i))}
          </ol>
          ${plan.svg ? html`<div class="diagram" aria-hidden="true">${unsafeHTML(plan.svg)}</div>` : nothing}
        </jh-card>
        ${this._diffSection(previous, diff)}`,
    });
  }
}
customElements.define('box-plan', BoxPlan);

/** Submit: the delegation verb as a form. */
export class BoxSubmit extends LitElement {
  static styles = [base, css`
    :host { display: block; }
    form { display: grid; gap: var(--jh-space-400); max-width: 560px; }
    form .actions { padding-top: var(--jh-space-200); }
  `];
  static properties = {
    _routes: { state: true }, _route: { state: true }, _key: { state: true },
    _payload: { state: true }, _result: { state: true }, _error: { state: true },
    _pending: { state: true },
  };

  constructor() {
    super();
    this._routes = null; this._route = ''; this._key = ''; this._payload = '';
    this._result = null; this._error = ''; this._pending = false;
  }

  connectedCallback() {
    super.connectedCallback();
    getStatus().then((s) => {
      this._routes = s.routes ?? [];
      if (!this._route && this._routes.length) this._route = this._routes[0];
    }).catch((e) => { this._error = e.message; this._routes = []; });
  }

  async _submit(e) {
    e.preventDefault();
    this._pending = true; this._error = ''; this._result = null;
    try {
      this._result = await openIngress(this._route, this._key.trim(), this._payload);
      toast(`${this._result.opened ? 'Opened' : 'Already open'}: ${title(this._payload)}`, 'positive');
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
          ${/* The radio group is rendered only once the routes exist: an
               empty group throws in jh-ui's roving tabindex. */ ''}
          ${this._routes === null
            ? loading('routes')
            : this._routes.length === 0
              ? html`<jh-notification appearance="neutral" type="alert" hide-dismiss-button>No routes are configured, so nothing can be submitted. Routes are operator configuration in box.yaml.</jh-notification>`
              : html`
                <jh-radio-group label="Route" name="route" .value=${this._route}
                  helper-text="Routes are the operator's configuration; work inherits the route's target and authority"
                  @jh-change=${(e) => { this._route = e.target.value; }}>
                  ${this._routes.map((r) => html`<jh-radio label=${r} value=${r} ?checked=${r === this._route}></jh-radio>`)}
                </jh-radio-group>`}
          <jh-input label="Occurrence key" required
            helper-text="The same key always names the same work — redelivery is safe"
            .value=${this._key}
            @jh-change=${(e) => { this._key = e.target.value; }}
            @input=${(e) => { this._key = e.composedPath()[0].value ?? this._key; }}></jh-input>
          <jh-input type="textarea" label="Task" rows="5" required
            helper-text="What should be done, in plain words. The first line is how the room will name it."
            .value=${this._payload}
            @jh-change=${(e) => { this._payload = e.target.value; }}
            @input=${(e) => { this._payload = e.composedPath()[0].value ?? this._payload; }}></jh-input>
          <div class="actions">
            <jh-button submit appearance="primary" label="Open ingress"
              ?pending=${this._pending} ?disabled=${!this._route || !this._key.trim() || !this._payload.trim()}></jh-button>
            ${!this._route || !this._key.trim() || !this._payload.trim()
              ? html`<span class="helper">Needs a route, a key and a task.</span>` : nothing}
          </div>
        </form>
        ${this._error ? html`<jh-notification appearance="negative" type="alert" hide-dismiss-button>${this._error}</jh-notification>` : nothing}
        ${this._result ? html`
          <jh-notification appearance="positive" type="alert" hide-dismiss-button>
            ${this._result.opened ? 'Opened' : 'Already open'} — item
            <a href=${'/items/' + this._result.item}>${this._result.item}</a>
          </jh-notification>` : nothing}
      </jh-card>`;
  }
}
customElements.define('box-submit', BoxSubmit);

export { FILTERS, clip, focusInto };
