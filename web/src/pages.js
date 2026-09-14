// The views. Each is one question an operator brings to the room, answered
// in words: what is the factory doing, what could it be doing, what is each
// agent saying, what needs a person, what happened to this unit, what is
// recorded about this commit, and how do I hand it more work.
import { LitElement, html, css, nothing } from 'lit';
import { unsafeHTML } from 'lit/directives/unsafe-html.js';
import { Task } from '@lit/task';
import {
  getStatus, getItems, getExceptions, getFlakes, getSections, getItem, getItemEvidence, getIngress, getProvenance, getProvenanceChain, provenanceExportURL,
  getSessions, getSession, openIngress,getRunFrames, reload, retryItem, resolveItem,
  getPlan, getIngressPlans, getPlanDiff, getOrigins, originsExportURL, getLandings,
} from './api.js';
import { ActivityController } from './sse.js';
import {
  age, title, taskURL, short, sameCommit, fmtTokens, fmtUSD, fmtCost, tokenSplit, breakdown, origin, source, ask, place, hold, why,
  describe, itemOf, appearanceOf, STATE_APPEARANCE, SEVERITY_APPEARANCE, PRIORITY_APPEARANCE, firstLine, clip, toolDetail, cardTitle,
  sessionHeading, currentSession, cardStage, roleSplit, roleSplitLine, ROLE_APPEARANCE, factCite, originTotals, perLandingCost, landingRow,
} from './format.js';
import { settled, orderList, planeOrder } from './order.js';
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
// Mirrors queue.Priority.Rank() on the wire: unset or unrecognised ranks as
// 'normal', the same fallback OrDefault gives the pump's own claim order —
// absence must never sort as though it were the lowest priority there is.
const PRIORITY_RANK = { low: 1, normal: 2, high: 3, urgent: 4 };

const COLUMNS = [
  { key: 'state', label: 'State', get: (e) => e.state },
  { key: 'work', label: 'Work', get: (e) => title(e) || e.id },
  { key: 'why', label: 'Blocked by', get: (e) => why(e) },
  { key: 'stage', label: 'Stage', get: (e) => e.stage ?? '' },
  { key: 'holder', label: 'Held by', get: (e) => e.runner ?? '' },
  { key: 'origin', label: 'Origin', get: (e) => origin(e) },
  { key: 'priority', label: 'Priority', get: (e) => PRIORITY_RANK[e.priority ?? 'normal'] ?? PRIORITY_RANK.normal },
  { key: 'age', label: 'Age', get: (e) => e.age_sec ?? 0 },
];

const stateTag = (state) => html`<jh-tag size="small" class=${STATE_APPEARANCE[state] ?? 'neutral'} label=${state}></jh-tag>`;
// Normal is the silent default: only a row that actually differs gets a tag.
const priorityTag = (p) => (p in PRIORITY_APPEARANCE ? html`<jh-tag size="small" class=${PRIORITY_APPEARANCE[p]} label=${p}></jh-tag>` : nothing);
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
  // The running tile's own seat fraction comes off the capacity report and
  // says nothing about who holds each seat — this is the listing roleSplit
  // projects to say the rest, fetched here rather than derived from spend
  // so the split can never drift from what /sessions itself would show.
  _sessions = new Task(this, { task: async (_, { signal }) => getSessions(signal), args: () => [] });
  _activity = new ActivityController(this, { onEvent: () => this._refresh() });

  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(() => { this._status.run(); this._items.run(); this._sessions.run(); }, 400);
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
    // A projection of the sessions listing itself, never spend.running: the
    // seat fraction above says how many seats are full, this says who is in
    // them. Undefined while the sessions fetch is still pending renders as
    // no line at all rather than a false "nobody running".
    const split = this._sessions.value ? roleSplitLine(roleSplit(this._sessions.value)) : '';
    return html`
      <div class="tiles">
        ${this._tile(s.pending, 'pending', { href: '/work', sub: 'owed a run' })}
        ${this._tile(s.exceptions, 'exceptions', { href: '/exceptions', sub: 'waiting on a person', tone: s.exceptions ? 'negative' : '' })}
        ${q?.parked
          ? this._tile(age(q.oldest_age_sec), 'oldest parked', { href: '/exceptions', sub: `objective ${age(q.objective_sec)}`, tone: q.met ? '' : 'negative' })
          : nothing}
        ${this._tile(s.claims?.stopped ? 'Stopped' : 'Running', 'claims', {
          sub: s.claims?.actor ? `by ${s.claims.actor}` : 'never touched', tone: s.claims?.stopped ? 'negative' : 'positive' })}
        ${sp?.running_builds != null
          ? this._tile(`${sp.running_builds}/${sp.max_concurrent}`, 'building now', {
              href: '/sessions',
              sub: [split, sp.started ? `${sp.started} started this window` : ''].filter(Boolean).join(' · '),
            })
          : nothing}
        ${sp?.running_judgements != null
          ? this._tile(`${sp.running_judgements}/${sp.max_judgements}`, 'judging now', { href: '/sessions' })
          : nothing}
        ${sp ? this._tile(fmtTokens(sp.tokens), 'tokens', { sub: sp.max_tokens ? `of ${fmtTokens(sp.max_tokens)} this window` : 'this window' }) : nothing}
        ${sp && (sp.cost_micros || sp.max_cost_micros)
          ? this._tile(fmtUSD(sp.cost_micros), 'spend', { sub: sp.max_cost_micros ? `of ${fmtUSD(sp.max_cost_micros)}` : 'this window' })
          : nothing}
        ${sp?.per_landing
          ? this._tile(perLandingCost(sp.per_landing).text, 'cost / landing', {
              href: '/landings',
              sub: sp.per_landing.unknown
                ? `${sp.per_landing.unknown} of ${sp.per_landing.count} unknown this window`
                : `${sp.per_landing.count} landed this window`,
            })
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
        <dd>${s.runner} ${s.version}, up ${age(s.uptime_sec)}, serving tenant <b>${s.tenant}</b> box <b>${s.box}</b>
          ${s.build_commit ? html` · runs <b>${short(s.build_commit)}</b>${s.build_dirty ? ' (dirty)' : ''}` : nothing}
          ${s.follow_branch ? html` · <b>${s.follow_branch}</b> at <b>${short(s.follow_head)}</b>${
            s.build_commit && !sameCommit(s.build_commit, s.follow_head)
              ? html` <span class="muted">(behind)</span>` : nothing}` : nothing}
        </dd>
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
        ${live.map((e) => {
          const t = cardTitle(title(e) || e.id);
          return html`
          <jh-list-item
            primary-text=${t.label}
            title=${t.full}
            secondary-text=${e.note ? firstLine(e.note) : origin(e) || place(e)}
            primary-metadata=${e.stage ?? ''}
            secondary-metadata=${e.stage ? `${age(e.in_state_sec)} in stage` : `${age(e.age_sec)} old`}
            show-divider>
            <span slot="jh-list-item-left">${stateTag(e.state)}</span>
            <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
              label="Open" accessible-label=${`Open ${t.full}`} href=${'/items/' + e.id}></jh-button>
          </jh-list-item>`;
        })}
      </jh-list-group>`;
  }

  // Always newest first, and only ever that: this is an SSE stream with no
  // stored state to settle into, so there is no branch to add — see
  // order.js and issue #205 for the surfaces that do have one.
  _feed(titles) {
    const evs = this._activity.events.slice().reverse();
    if (evs.length === 0) return html`<p class="empty">Nothing yet.</p>`;
    return html`
      <ol class="feed" aria-label="Activity feed, newest first">
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
    // No sort until asked: the server's order is priority then age, which is
    // the order the factory would take the work in and the only one that is
    // a fact about the queue rather than a preference of the reader.
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
          <caption>${rows.length} item(s), ${this._sort ? `sorted by ${this._sort}` : 'priority, then age'}</caption>
          <colgroup>
            <col style="width: 108px"><col><col style="width: 150px"><col style="width: 110px">
            <col style="width: 160px"><col style="width: 150px"><col style="width: 90px"><col style="width: 64px">
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
                <td>${priorityTag(e.priority)}</td>
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

/** groupFramesBySession buckets frames by the agentref that produced them —
 *  never by time, per issue #107. A frame with no `session` predates
 *  attribution and goes to `unattributed`, first and on its own; every other
 *  frame goes to the bucket named by its agentref, whether or not that
 *  agentref is one of this scope's known AgentSessions rows (a session the
 *  chain has not caught up to yet still gets its own bucket rather than
 *  losing its frames). Each bucket is `collapse`d on its own, so a tool call
 *  announced and titled inside one session folds into one row exactly as it
 *  always has. */
export function groupFramesBySession(frames) {
  const unattributed = [];
  const byRef = new Map();
  const order = [];
  for (const f of frames ?? []) {
    const ref = f.session || '';
    if (!ref) { unattributed.push(f); continue; }
    if (!byRef.has(ref)) { byRef.set(ref, []); order.push(ref); }
    byRef.get(ref).push(f);
  }
  for (const ref of order) byRef.set(ref, collapse(byRef.get(ref)));
  return { unattributed: collapse(unattributed), byRef, order };
}

/** continuationTree links each AgentSession row that continued a prior ACP
 *  session to the row that opened it, so a rebuild's second attempt renders
 *  nested under the first rather than as a fresh top-level card. Linked by
 *  the ACP session id (`continues` naming another row's `session`), which is
 *  a different value than the agentref `groupFramesBySession` keys frames
 *  by. Rows named by no other row's session — including every row when
 *  `sessions` carries no `session`/`continues` data at all — come back as
 *  roots, in their given order. */
export function continuationTree(sessions) {
  // First-wins: a resumed attempt's agent-session record carries the SAME
  // ACP session id it continued (see acp.Builder), so with an attempts
  // budget of 4+ several rows can share one `session` value. The first row
  // to open that session is the one a later row's `continues` should
  // resolve to — a later write must not steal the entry and misattribute
  // who continued whom.
  const openedBy = new Map();
  for (const s of sessions ?? []) if (s.session && !openedBy.has(s.session)) openedBy.set(s.session, s.agentref);
  const childrenOf = new Map();
  const roots = [];
  for (const s of sessions ?? []) {
    const parent = s.continues ? openedBy.get(s.continues) : undefined;
    if (parent && parent !== s.agentref) {
      if (!childrenOf.has(parent)) childrenOf.set(parent, []);
      childrenOf.get(parent).push(s);
    } else {
      roots.push(s);
    }
  }
  return { roots, childrenOf };
}

/** sessionForest is groupFramesBySession and continuationTree composed: every
 *  row this scope's frames named, known AgentSessions rows first in chain
 *  order and any agentref frames named that the chain has not caught up to
 *  after, as synthetic rows carrying nothing but that agentref. Callers walk
 *  `roots` and recurse into `childrenOf` to render the whole tree; frames for
 *  a row are `byRef.get(row.agentref) ?? []`. */
export function sessionForest(sessions, frames) {
  const { unattributed, byRef, order } = groupFramesBySession(frames);
  const known = new Set((sessions ?? []).map((s) => s.agentref));
  const extra = order.filter((ref) => !known.has(ref)).map((ref) => ({ agentref: ref }));
  const { roots, childrenOf } = continuationTree([...(sessions ?? []), ...extra]);
  return { unattributed, byRef, roots, childrenOf };
}

/** frameList is the ol.frames markup itself, factored out so a session
 *  section and the flat pages that came before it render one agent's
 *  timeline identically: a call folded to one row by `collapse`, denials and
 *  escalations picked out by FRAME_APPEARANCE. `frames` must already be in
 *  render order — callers get that from `planeOrder`/`orderList` (see
 *  order.js) — this only renders it and labels which order is in force, per
 *  issue #205: live reads newest first, settled reads from the start. */
function frameList(frames, emptyMessage, live) {
  return frames.length === 0
    ? html`<p class="empty">${emptyMessage}</p>`
    : html`<ol class="frames" aria-label=${live ? 'Agent timeline, newest first' : 'Agent timeline, from the start'}>${frames.map((f) => html`
        <li class=${FRAME_APPEARANCE[f.kind] ?? ''}>
          <span class="t"><box-when .at=${f.at}></box-when></span>
          <span class="k">${f.kind}</span>
          <span class="x">${frameLine(f)}</span>
        </li>`)}</ol>`;
}

/** sessionHeadingSpan draws one agent-session row's own header from
 *  [sessionHeading]: the role's own tag, then either a review's verdict —
 *  pass or fail, its reason's first sentence, and a link to the gate record
 *  — or the row's own headline (an attempt, a built sha, "reviewing
 *  <sha>"). Shared by the sessions page and the item page so the same row
 *  never draws two ways; `verdictHref` differs only in whether the link
 *  crosses pages (the sessions page) or stays on this one (the item page). */
function sessionHeadingSpan(row, verdictHref) {
  const h = sessionHeading(row);
  return html`
    <jh-tag size="small" class=${h.roleAppearance} label=${h.role}></jh-tag>
    ${h.verdict
      ? html`
        <jh-tag size="small" class=${h.verdict.appearance} label=${h.verdict.decision}></jh-tag>
        ${h.verdict.reason ? html`<span>${h.verdict.reason}</span>` : nothing}
        ${h.verdict.recordId ? html`<a href=${verdictHref(h.verdict.recordId)}>gate record</a>` : nothing}`
      : (h.text ? html`<span>${h.text}</span>` : nothing)}`;
}

/**
 * Sessions: one card per running unit — what it is, how far it has got, and
 * the sequence of agent sessions it is, issue #107's own framing.
 *
 * Cards side by side rather than one merged feed, because fan-out is the case
 * a feed breaks on: several runs interleave into a stream that reads as noise,
 * and the question an operator has — is THIS unit moving — is then answered by
 * scrolling. Inside a card, what used to be one interleaved timeline is now
 * one section per agent session — same grouping box-item uses, since a unit
 * here and a unit there are the same chain read two ways.
 */
export class BoxSessions extends LitElement {
  static styles = [base, css`
    :host { display: block; }
    .grid { display: grid; gap: var(--jh-space-400); grid-template-columns: repeat(auto-fit, minmax(min(380px, 100%), 1fr)); align-items: start; }
    .grid > * { min-width: 0; }
    /* The title band says who holds the run before a word does — a border
       in the role's own colour, the same ones jh-tag.brand/positive use. */
    jh-card.role-builder { border-left: var(--jh-space-100) solid var(--jh-color-content-brand-enabled); }
    jh-card.role-gate { border-left: var(--jh-space-100) solid var(--jh-color-content-secondary-enabled); }
    jh-card.role-planner { border-left: var(--jh-space-100) solid var(--jh-color-content-positive-enabled); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); margin-bottom: var(--jh-space-200); }
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
    .layers { display: flex; gap: var(--jh-space-100); flex-wrap: wrap; margin-bottom: var(--jh-space-200); }
    .layer { font: var(--jh-font-micro-medium); padding: 2px var(--jh-space-200); border-radius: var(--jh-border-radius-100);
             background: var(--jh-color-container-secondary-enabled); }
    h4 { margin: var(--jh-space-300) 0 var(--jh-space-100); font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    ol.frames { list-style: none; margin: 0; padding: 0; max-height: 320px; overflow-y: auto; font: var(--jh-font-helper-regular); }
    ol.frames li { display: grid; grid-template-columns: 5.5em 6em 1fr; gap: var(--jh-space-200); padding: 3px 0;
                   border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.frames .x b { font-weight: 600; }
    ol.frames .t { color: var(--jh-color-content-secondary-enabled); font-variant-numeric: tabular-nums; }
    ol.frames .k { font-weight: 600; }
    ol.frames .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    ol.frames li.negative .k { color: var(--jh-color-content-negative-enabled); }
    ol.frames li.positive .k { color: var(--jh-color-content-positive-enabled); }
    .blind { color: var(--jh-color-content-secondary-enabled); font-style: italic; margin: 0; }
    details.session { margin: var(--jh-space-300) 0 0; padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
                       background: var(--jh-color-container-secondary-enabled); }
    details.session details.session { margin-left: var(--jh-space-400); }
    .session-meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: baseline;
                    font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .session-meta > * { min-width: 0; overflow-wrap: anywhere; }
    .session-meta b { color: var(--jh-color-content-primary-enabled); font-weight: 600; }
    .session-meta .running { color: var(--jh-color-content-positive-enabled); font-weight: 600; }
  `];
  static properties = { _frames: { state: true } };

  constructor() {
    super();
    // Frames arriving on the stream, keyed by unit, merged onto whatever the
    // last fetch returned. Without this a card would only move as fast as the
    // roster refetch, which is the reload the page exists to replace.
    this._frames = {};
    // The agentrefs the last fetch knew about. A frame naming one outside
    // this set belongs to a session the chain has recorded since — the
    // listing itself is stale, not just missing a card to paint the frame
    // into, so this refetches rather than quietly buffering the frame.
    this._known = new Set();
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: () => this._refresh(),
      onSession: (f) => {
        this._frames = { ...this._frames, [f.item]: [...(this._frames[f.item] ?? []), f] };
        if (f.session && !this._known.has(f.session)) this._refresh();
      },
    });
  }

  _task = new Task(this, {
    task: async (_, { signal }) => {
      const rows = await getSessions(signal);
      this._known = new Set(rows.flatMap((s) => (s.agent_sessions ?? []).map((a) => a.agentref)));
      return rows;
    },
    args: () => [],
  });

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
  // and makes a quiet run look busy. Left uncollapsed: collapse() runs per
  // session inside sessionForest, once frames are grouped rather than merged
  // flat.
  _liveFrames(s) {
    const seen = new Set((s.timeline ?? []).map((f) => f.seq));
    return [...(s.timeline ?? []), ...(this._frames[s.id] ?? []).filter((f) => !seen.has(f.seq))];
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

  _sessionMeta(s, itemId) {
    return html`<span class="session-meta">
      ${sessionHeadingSpan(s, (recordId) => `/items/${itemId}#gate-${recordId}`)}
      ${s.model ? html`<span>${s.model}</span>` : nothing}
      ${s.agentref ? html`<span><box-hash label="agentref" .value=${s.agentref}></box-hash></span>` : nothing}
      ${s.started
        ? (s.finished ? html`<span>ended <box-when .at=${s.ended}></box-when></span>` : html`<span class="running">running</span>`)
        : nothing}
      ${s.finished ? html`<span>${fmtTokens(s.tokens)} tokens · ${fmtCost(s.cost_usd)}</span>` : nothing}
    </span>`;
  }

  _sessionRow(s, childrenOf, framesOf, live, itemId) {
    const frames = framesOf(s.agentref);
    const children = childrenOf.get(s.agentref) ?? [];
    return html`
      <details class="session" ?open=${!s.finished}>
        <summary>${this._sessionMeta(s, itemId)}</summary>
        ${frameList(frames, 'Nothing said yet.', live)}
        ${children.map((c) => this._sessionRow(c, childrenOf, framesOf, live, itemId))}
      </details>`;
  }

  _card(s) {
    const frames = this._liveFrames(s);
    const { unattributed, byRef, roots, childrenOf } = sessionForest(s.agent_sessions, frames);
    // Every row here is a unit the sessions page is showing because it is
    // running, but the flip is the shared predicate, not that assumption —
    // a judgement row carries no `state` at all, and `settled(undefined, …)`
    // correctly reads that as live rather than needing its own branch.
    const live = !settled(s.state, s.agent_sessions);
    const order = planeOrder(live, roots, childrenOf, (ref) => byRef.get(ref) ?? []);
    const earlier = unattributed.length ? html`
      <details class="session" open>
        <summary><span class="session-meta"><b>Earlier, unattributed</b></span></summary>
        ${frameList(orderList(live, unattributed), 'Nothing here.', live)}
      </details>` : nothing;
    const t = cardTitle(title(s) || s.id);
    // The current session is who holds the run right now — the title band
    // says so at a glance, and the meta line below says so in words.
    const cur = currentSession(s.agent_sessions);
    const roleClass = cur ? `role-${cur.role}` : '';
    const stage = cardStage(s);
    return html`
      <jh-card class=${roleClass} header-title=${t.label} title=${t.full} header-subtitle=${origin(s) || place(s)} padding="medium">
        <div class="meta">
          ${s.judgement ? html`<jh-tag size="small" label="judgement"></jh-tag>` : stateTag(s.state)}
          ${cur ? html`<jh-tag size="small" class=${ROLE_APPEARANCE[cur.role] ?? 'neutral'} label=${cur.role}></jh-tag>` : nothing}
          ${stage
            ? html`<jh-tag size="small" label=${stage.model ? `${stage.label} — ${stage.model}` : stage.label}></jh-tag>
                   ${s.judgement ? nothing : html`<span>${age(s.in_state_sec)} in stage</span>`}`
            : nothing}
          ${s.seat ? html`<span>seat <b>${s.seat}</b></span>` : nothing}
          ${s.runner ? html`<span>${hold(s)}</span>` : nothing}
        </div>
        <div class="meta">
          ${s.judgement
            ? html`<span>ingress <box-hash label="ingress id" .value=${s.id} href=${'/ingress/' + s.id} chars="18"></box-hash></span>`
            : html`<span>item <box-hash label="item id" .value=${s.id} href=${'/items/' + s.id} chars="18"></box-hash></span>`}
          ${s.agent_tokens ? html`<span>${fmtTokens(s.agent_tokens)} tokens · ${fmtCost(s.agent_cost_usd)} across every session below</span>` : nothing}
          ${s.head ? html`<span>head <box-hash label="commit" .value=${s.head} href=${'/provenance/' + s.head}></box-hash></span>` : nothing}
        </div>
        ${/* Which layers, in which order — the agentref is a digest and alone
             it answers nothing about what the agent actually was. */ ''}
        ${s.layers?.length
          ? html`<div class="layers" aria-label="Agent layers">${s.layers.map((l) => html`
              <span class="layer" title=${`${l.source} ${l.digest}`}>${l.scope}:${l.name}</span>`)}</div>`
          : nothing}
        <h4>Agent sessions <span class="helper">— ${live ? 'live, newest first' : 'finished, from the start'}</span></h4>
        ${unattributed.length === 0 && order.roots.length === 0
          ? (s.watching
              ? html`<p class="empty">Started; nothing said yet.</p>`
              : html`<p class="blind">No live stream here — another runner holds this, or the window has rolled.
                     Silence about the run, not an idle agent.</p>`)
          : html`
            ${live ? nothing : earlier}
            ${order.roots.map((row) => this._sessionRow(row, order.childrenOf, order.framesOf, live, s.id))}
            ${live ? earlier : nothing}`}
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
// exceptionCounts words the summary honestly: parked and failed are different
// debts (one is still running, one stopped), so a queue that is all of one
// kind should not imply the other is present at zero.
const exceptionCounts = (items) => {
  const parked = items.filter((i) => i.state !== 'failed').length;
  const failed = items.length - parked;
  if (parked && failed) return html`<span><b>${parked}</b> parked · <b>${failed}</b> failed</span>`;
  if (failed) return html`<span><b>${failed}</b> failed</span>`;
  return html`<span><b>${parked}</b> parked</span>`;
};

export class BoxExceptions extends LitElement {
  static styles = [base, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); margin-bottom: var(--jh-space-300); }
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
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
        <jh-card header-title="Exception queue" header-subtitle="Parked and failed work waiting on a person, loudest and oldest first" padding="medium">
          ${(items?.length ?? 0) === 0
            ? html`<p class="empty">Nothing here — the factory has nothing waiting on a person.</p>`
            : html`<div class="summary">
                ${exceptionCounts(items)}
                <span class=${items.some((i) => i.overdue) ? 'overdue' : ''}><b>${items.filter((i) => i.overdue).length}</b> past the response-time objective</span>
                <span class="helper">Retry hands the unit back to the factory. Resolve closes it with your reason on the record.</span>
              </div>`}
        </jh-card>
        ${(items ?? []).map((it) => this._card(it))}`,
    });
  }

  _card(it) {
    const t = cardTitle(title(it) || it.id);
    return html`
      <jh-card header-title=${t.label} title=${t.full} header-subtitle=${origin(it) || place(it)} padding="medium">
        <div class="meta">
          ${stateTag(it.state ?? 'parked')}
          <jh-tag size="small" class=${SEVERITY_APPEARANCE[it.severity ?? 'medium'] ?? 'neutral'} label=${`severity ${it.severity ?? 'medium'}`}></jh-tag>
          ${it.stage ? html`<jh-tag size="small" label=${`at ${it.stage}`}></jh-tag>` : nothing}
          <span>${it.state === 'failed' ? 'failed' : 'parked'} <box-when .at=${it.parked}></box-when></span>
          ${it.overdue ? html`<span class="overdue">past the response-time objective</span>` : nothing}
          ${it.escalations ? html`<span>escalated out of band ${it.escalations}×</span>` : nothing}
          ${ask(it) ? html`<span>ask ${ask(it)}</span>` : nothing}
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

/**
 * Flakes: every test the loop has proven flaky — failed once, then passed
 * unchanged on a rebuild — newest occurrence first. One row per test, with
 * how many times it has proven flaky and the two commits its latest proof
 * named: the head the gate refused, and the head that later passed.
 */
export class BoxFlakes extends LitElement {
  static styles = [base, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); }
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
    .summary { display: flex; gap: var(--jh-space-400); flex-wrap: wrap; align-items: center; }
  `];

  _task = new Task(this, { task: async (_, { signal }) => getFlakes(signal), args: () => [] });

  constructor() {
    super();
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (/index\/flakes/.test(a.scope)) this._refresh(); },
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

  render() {
    return this._task.render({
      pending: () => html`<jh-card header-title="Flaky tests" padding="medium">${loading('flakes')}</jh-card>`,
      error: (e) => html`<jh-card header-title="Flaky tests" padding="medium">${failed(e)}</jh-card>`,
      complete: (items) => html`
        <jh-card header-title="Flaky tests" header-subtitle="Proven flaky at least once, newest occurrence first" padding="medium">
          ${(items?.length ?? 0) === 0
            ? html`<p class="empty">Nothing here — no test has failed and then passed unchanged.</p>`
            : html`<div class="summary">
                <span><b>${items.length}</b> test${items.length === 1 ? '' : 's'}</span>
                <span><b>${items.reduce((n, i) => n + (i.count ?? 0), 0)}</b> proofs total</span>
              </div>`}
        </jh-card>
        ${(items ?? []).map((it) => this._card(it))}`,
    });
  }

  _card(it) {
    const t = cardTitle(it.test || it.subject);
    return html`
      <jh-card header-title=${t.label} title=${t.full} header-subtitle=${[it.repo, it.package].filter(Boolean).join(' / ')} padding="medium">
        <div class="meta">
          <jh-tag size="small" label=${`flaky ${it.count}×`}></jh-tag>
          <span>last proven <box-when .at=${it.last_seen}></box-when></span>
          ${it.work_item ? html`<span>on <a href=${'/items/' + it.work_item}>${it.work_item}</a></span>` : nothing}
        </div>
        <div class="meta">
          ${it.refused_head
            ? html`<span>refused at <box-hash label="commit" .value=${it.refused_head} href=${'/provenance/' + it.refused_head}></box-hash></span>`
            : nothing}
          ${it.passed_head
            ? html`<span>passed at <box-hash label="commit" .value=${it.passed_head} href=${'/provenance/' + it.passed_head}></box-hash></span>`
            : nothing}
        </div>
      </jh-card>`;
  }
}
customElements.define('box-flakes', BoxFlakes);

/**
 * Sections: the per-repository census GET /v1/sections folds from the
 * landed chain (issue #200) — what has landed in each declared section, how
 * long units waited to get one, and which sections tend to land together.
 * Declared sections a repo has never touched still appear, at zero; the
 * whole-repository and unsectioned buckets appear too, when the chain
 * actually produced them, because the cost of "no section" is as real as
 * any section's own tally. Any graph here would be a client-side diagram of
 * the co-change pairs, which docs/PLAN.md §6 settles server-side in Go — a
 * plain table is the record.
 */
export class BoxSections extends LitElement {
  static styles = [base, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .reading { margin: 0; font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .wrap { overflow-x: auto; }
    .wrap + .wrap { margin-top: var(--jh-space-300); }
    table { border-collapse: collapse; width: 100%; min-width: 420px; }
    caption { text-align: left; padding-bottom: var(--jh-space-200); font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    th, td { text-align: left; vertical-align: top; padding: var(--jh-space-200) var(--jh-space-300) var(--jh-space-200) 0;
             border-bottom: 1px solid var(--jh-color-divider-secondary); }
    thead th { white-space: nowrap; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    tbody th { font-weight: 500; }
  `];

  _task = new Task(this, { task: async (_, { signal }) => getSections(signal), args: () => [] });

  constructor() {
    super();
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (/index\/landings/.test(a.scope)) this._refresh(); },
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

  render() {
    return this._task.render({
      pending: () => html`<jh-card header-title="Sections" padding="medium">${loading('sections')}</jh-card>`,
      error: (e) => html`<jh-card header-title="Sections" padding="medium">${failed(e)}</jh-card>`,
      complete: (byRepo) => this._body(byRepo ?? {}),
    });
  }

  _body(byRepo) {
    const repos = Object.keys(byRepo).sort();
    return html`
      <jh-card header-title="Sections" header-subtitle="What has landed where, what waited, and what changes together" padding="medium">
        <p class="reading">A section that is always waited on and never co-changes with its neighbours is a lease to
          split; two sections that always change together are a lease to merge.</p>
      </jh-card>
      ${repos.length === 0
        ? html`<jh-card padding="medium"><p class="empty">No repository has a section census yet.</p></jh-card>`
        : repos.map((repo) => this._repo(repo, byRepo[repo]))}`;
  }

  _repo(repo, census) {
    const sections = census?.sections ?? [];
    const coChanges = census?.co_changes ?? [];
    return html`
      <jh-card header-title=${repo} padding="medium">
        ${sections.length === 0
          ? html`<p class="empty">Nothing has landed or waited here yet.</p>`
          : html`
            <div class="wrap">
              <table>
                <caption>${sections.length} section${sections.length === 1 ? '' : 's'}</caption>
                <thead>
                  <tr><th scope="col">Section</th><th scope="col">Landed</th><th scope="col">Waits</th><th scope="col">Wait time</th></tr>
                </thead>
                <tbody>
                  ${sections.map((s) => html`
                    <tr>
                      <th scope="row">${s.name}</th>
                      <td>${s.landed}</td>
                      <td>${s.waits}</td>
                      <td>${s.waits === 0
                        ? html`<span class="muted">—</span>`
                        : s.wait_seconds != null ? age(s.wait_seconds) : 'unknown'}</td>
                    </tr>`)}
                </tbody>
              </table>
            </div>`}
        ${coChanges.length > 0 ? html`
          <div class="wrap">
            <table>
              <caption>${coChanges.length} co-change pair${coChanges.length === 1 ? '' : 's'}</caption>
              <thead><tr><th scope="col">Sections</th><th scope="col">Times</th></tr></thead>
              <tbody>
                ${coChanges.map((c) => html`
                  <tr><th scope="row">${c.sections.join(' + ')}</th><td>${c.count}</td></tr>`)}
              </tbody>
            </table>
          </div>` : nothing}
      </jh-card>`;
  }
}
customElements.define('box-sections', BoxSections);

/**
 * Landings: GET /v1/landings, newest first — the per-item counterpart to
 * the dashboard's cost-per-landing tile (issue #183). Cost, tokens, attempt
 * and review count per row, and the fold both cutoffs (the meter's window,
 * and since this daemon booted) already produced, never re-derived here.
 * A row whose cost the fold could not price shows 'unknown' in place of the
 * money and token cells — fmtUSD's null-coerces-to-zero is exactly the trap
 * this page exists to avoid, so every cell goes through [landingRow] first.
 */
export class BoxLandings extends LitElement {
  static styles = [base, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .reading { margin: 0; font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .wrap { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; min-width: 560px; }
    caption { text-align: left; padding-bottom: var(--jh-space-200); font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    th, td { text-align: left; vertical-align: top; padding: var(--jh-space-200) var(--jh-space-300) var(--jh-space-200) 0;
             border-bottom: 1px solid var(--jh-color-divider-secondary); }
    thead th { white-space: nowrap; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    tbody th { font-weight: 500; }
    .unknown { color: var(--jh-color-content-secondary-enabled); }
  `];

  _task = new Task(this, { task: async (_, { signal }) => getLandings(signal), args: () => [] });

  constructor() {
    super();
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (/index\/landings/.test(a.scope)) this._refresh(); },
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

  render() {
    return this._task.render({
      pending: () => html`<jh-card header-title="Landings" padding="medium">${loading('landings')}</jh-card>`,
      error: (e) => html`<jh-card header-title="Landings" padding="medium">${failed(e)}</jh-card>`,
      complete: (page) => this._body(page ?? {}),
    });
  }

  _body(page) {
    const landings = page.landings ?? [];
    const sinceBoot = perLandingCost(page.since_boot);
    const windowFold = page.window ? perLandingCost(page.window) : null;
    const rows = landings.map(landingRow);
    const unknown = rows.filter((r) => !r.known).length;
    return html`
      <jh-card header-title="Landings" header-subtitle="Every settled item, newest first, with what it cost to land" padding="medium">
        <p class="reading">
          Since boot: ${sinceBoot.text}${page.since_boot?.count ? ` across ${page.since_boot.count} landing${page.since_boot.count === 1 ? '' : 's'}` : ''}${
            page.since_boot?.unknown ? ` (${page.since_boot.unknown} unknown)` : ''}.
          ${windowFold ? html`This window: ${windowFold.text}${page.window.unknown ? ` (${page.window.unknown} of ${page.window.count} unknown)` : ''}.` : nothing}
        </p>
      </jh-card>
      ${landings.length === 0
        ? html`<jh-card padding="medium"><p class="empty">Nothing has landed yet.</p></jh-card>`
        : html`
          <jh-card padding="medium">
            <div class="wrap">
              <table>
                <caption>${landings.length} landing${landings.length === 1 ? '' : 's'}${unknown ? ` · ${unknown} unknown cost` : ''}</caption>
                <thead>
                  <tr>
                    <th scope="col">Landed</th><th scope="col">Work</th><th scope="col">Commit</th>
                    <th scope="col">Cost</th><th scope="col">Tokens</th><th scope="col">Attempt</th><th scope="col">Reviews</th>
                  </tr>
                </thead>
                <tbody>
                  ${rows.map((r) => {
                    const t = cardTitle(r.line || r.id);
                    return html`
                      <tr>
                        <td><box-when .at=${r.at}></box-when></td>
                        <th scope="row"><a href=${'/items/' + r.id} title=${t.full}>${t.label}</a></th>
                        <td>${r.head ? html`<box-hash label="commit" .value=${r.head} href=${'/provenance/' + r.head}></box-hash>` : nothing}</td>
                        <td class=${r.known ? '' : 'unknown'}>${r.money}</td>
                        <td class=${r.known ? '' : 'unknown'}>${r.tokens}</td>
                        <td>${r.attempt}</td>
                        <td>${r.reviews}</td>
                      </tr>`;
                  })}
                </tbody>
              </table>
            </div>
          </jh-card>`}
    `;
  }
}
customElements.define('box-landings', BoxLandings);

/** One item: what it is, where it stands, what can be done, and its whole chain. */
export class BoxItem extends LitElement {
  static properties = { itemId: { attribute: false }, _live: { state: true } };
  static styles = [base, facts, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; margin-bottom: var(--jh-space-300); }
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
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
    details.session { margin: var(--jh-space-300) 0 0; padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
                       background: var(--jh-color-container-secondary-enabled); }
    details.session details.session { margin-left: var(--jh-space-400); }
    details.session summary { color: var(--jh-color-content-primary-enabled); }
    .session-meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: baseline;
                    font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .session-meta > * { min-width: 0; overflow-wrap: anywhere; }
    .session-meta b { color: var(--jh-color-content-primary-enabled); font-weight: 600; }
    .session-meta .running { color: var(--jh-color-content-positive-enabled); font-weight: 600; }
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
  // Left uncollapsed here — collapse() runs per session, inside
  // groupFramesBySession, once frames are grouped rather than merged flat.
  _frames(kept) {
    const seen = new Set((kept ?? []).map(frameKey));
    return [...(kept ?? []), ...this._live.filter((f) => !seen.has(frameKey(f)))];
  }

  // The agentref a session's header links to the record that explains what
  // it was — the agent-prompt record on this same page's Timeline card,
  // reached by fragment rather than a route this room does not have.
  _agentRefLink(ref, promptRefs) {
    return promptRefs.has(ref)
      ? html`<a href=${'#agent-prompt-' + ref}><box-hash label="agentref" .value=${ref}></box-hash></a>`
      : html`<box-hash label="agentref" .value=${ref}></box-hash>`;
  }

  // A row groupFramesBySession/continuationTree gave a real Started time is
  // one of this unit's own AgentSessions; one without is a synthesized stub
  // built from a frame naming a session the chain has not caught up to yet
  // — see sessionForest. The two render at different fidelity because that
  // is the most either can honestly say.
  _sessionSummary(s, promptRefs) {
    return html`<span class="session-meta">
      ${sessionHeadingSpan(s, (recordId) => `#gate-${recordId}`)}
      ${s.model ? html`<span>${s.model}</span>` : nothing}
      ${s.agentref ? html`<span>${this._agentRefLink(s.agentref, promptRefs)}</span>` : nothing}
      ${s.session ? html`<span><box-hash label="ACP session" .value=${s.session}></box-hash></span>` : nothing}
      ${s.started ? html`<span>started <box-when .at=${s.started}></box-when></span>` : nothing}
      ${s.started
        ? (s.finished ? html`<span>ended <box-when .at=${s.ended}></box-when></span>` : html`<span class="running">running</span>`)
        : nothing}
      ${s.finished ? html`<span>${fmtTokens(s.tokens)} tokens · ${fmtCost(s.cost_usd)}</span>` : nothing}
    </span>`;
  }

  // One session's own section, recursing into whatever continued it — see
  // continuationTree — so a rebuild's second attempt nests under the first
  // instead of reading as an unrelated card below it.
  _sessionCard(s, childrenOf, framesOf, promptRefs, live) {
    const frames = framesOf(s.agentref);
    const children = childrenOf.get(s.agentref) ?? [];
    return html`
      <details class="session" ?open=${!s.finished}>
        <summary>${this._sessionSummary(s, promptRefs)}</summary>
        ${frameList(frames, 'Nothing said yet.', live)}
        ${children.map((c) => this._sessionCard(c, childrenOf, framesOf, promptRefs, live))}
      </details>`;
  }

  _sessions(item, kept, evidence, live) {
    const frames = this._frames(kept);
    const { unattributed, byRef, roots, childrenOf } = sessionForest(item.agent_sessions, frames);
    const promptRefs = new Set((evidence ?? []).filter((r) => r.Kind === 'agent-prompt').map((r) => r.Subject));
    const order = planeOrder(live, roots, childrenOf, (ref) => byRef.get(ref) ?? []);
    const subtitle = live
      ? 'One section per agent session — live, newest first'
      : 'One section per agent session — finished, from the start';
    const empty = unattributed.length === 0 && order.roots.length === 0;
    const earlier = unattributed.length ? html`
      <details class="session" open>
        <summary><span class="session-meta"><b>Earlier, unattributed</b></span></summary>
        ${frameList(orderList(live, unattributed), 'Nothing here.', live)}
      </details>` : nothing;
    return html`
      <jh-card header-title="Agent" header-subtitle=${subtitle} padding="medium">
        ${empty
          ? html`<p class="empty">${live
              ? 'Nothing streamed yet. The first frame is the agent starting.'
              : 'Nothing was kept for this run — it ran before the run log existed, or its frames were pruned.'}</p>`
          : html`
            ${live ? nothing : earlier}
            ${order.roots.map((s) => this._sessionCard(s, order.childrenOf, order.framesOf, promptRefs, live))}
            ${live ? earlier : nothing}`}
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
    const t = cardTitle(title(item) || item.id);
    return html`
      <jh-card header-title=${t.label} title=${t.full} header-subtitle=${origin(item) || place(item)} padding="medium">
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
          ${item.agent_tokens ? html`<dt>Agent spend</dt><dd>${fmtTokens(item.agent_tokens)} tokens · ${fmtCost(item.agent_cost_usd)}
              <span class="helper">summed across every agent session below</span></dd>` : nothing}
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

  _chain(evidence, live) {
    if ((evidence?.length ?? 0) === 0) return html`<p class="empty">Nothing recorded yet.</p>`;
    return html`
      <ol class="chain" aria-label=${live ? 'Run timeline, newest first' : 'Run timeline, from the start'}>
        ${orderList(live, evidence).map((r) => html`
          <li id=${r.Kind === 'agent-prompt' ? 'agent-prompt-' + r.Subject : r.Kind === 'gate' ? 'gate-' + r.ID : nothing}>
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
      complete: ({ item, evidence, kept }) => {
        const live = !settled(item.state, item.agent_sessions);
        return html`
          ${this._head(item)}
          ${this._sessions(item, kept, evidence, live)}
          <jh-card header-title="Timeline" header-subtitle=${live
              ? 'What this unit did — live, newest first'
              : 'What this unit did — finished, from the start'} padding="medium">
            ${this._chain(evidence, live)}
          </jh-card>`;
      },
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
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
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
    details.session { margin: var(--jh-space-300) 0 0; padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
                       background: var(--jh-color-container-secondary-enabled); }
    .session-meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: baseline;
                    font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .session-meta > * { min-width: 0; overflow-wrap: anywhere; }
    .session-meta b { color: var(--jh-color-content-primary-enabled); font-weight: 600; }
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
  // frameKey the daemon stamps on both. Left uncollapsed: collapse() runs
  // per session inside sessionForest.
  _frames(kept) {
    const seen = new Set((kept ?? []).map(frameKey));
    return [...(kept ?? []), ...this._live.filter((f) => !seen.has(frameKey(f)))];
  }

  // A judgement has no AgentSessions rows to read — no queue item means no
  // /v1/items/{id}/session for control to answer from (IngressView carries
  // none either) — so every section here is the synthesized kind
  // sessionForest builds from a frame's own `session` field alone: an
  // agentref and nothing else. Same grouping box-item and box-sessions use,
  // at the fidelity a judgement's evidence actually supports. That fidelity
  // is also why the settled/live split here is `!ing.standing` alone rather
  // than the shared `settled()` predicate: a synthesized row carries no
  // `finished` for that predicate's second half to read, and a judgement
  // stops producing sessions the moment it gets a standing — the standing
  // itself already says whether there is more to come.
  _sessionCard(s, framesOf, live) {
    const frames = framesOf(s.agentref);
    return html`
      <details class="session" open>
        <summary><span class="session-meta"><b>agent</b> <box-hash label="agentref" .value=${s.agentref}></box-hash></span></summary>
        ${frameList(frames, 'Nothing said yet.', live)}
      </details>`;
  }

  _agent(ing, kept) {
    const frames = this._frames(kept);
    const { unattributed, byRef, roots, childrenOf } = sessionForest([], frames);
    const live = !ing.standing;
    const order = planeOrder(live, roots, childrenOf, (ref) => byRef.get(ref) ?? []);
    const subtitle = live
      ? 'One section per agent session — live, newest first'
      : 'One section per agent session — finished, from the start';
    const earlier = unattributed.length ? html`
      <details class="session" open>
        <summary><span class="session-meta"><b>Earlier, unattributed</b></span></summary>
        ${frameList(orderList(live, unattributed), 'Nothing here.', live)}
      </details>` : nothing;
    return html`
      <jh-card header-title="Agent" header-subtitle=${subtitle} padding="medium">
        ${unattributed.length === 0 && order.roots.length === 0
          ? html`<p class="empty">${live
              ? 'Nothing streamed yet. The first frame is the agent starting.'
              : 'Nothing was kept for this run — it ran before the run log existed, or its frames were pruned.'}</p>`
          : html`
            ${live ? nothing : earlier}
            ${order.roots.map((s) => this._sessionCard(s, order.framesOf, live))}
            ${live ? earlier : nothing}`}
      </jh-card>`;
  }

  _head(ing) {
    const running = !ing.standing;
    const t = cardTitle(title(ing) || ing.id);
    return html`
      <jh-card header-title=${t.label} title=${t.full} header-subtitle=${place(ing)} padding="medium">
        <div class="meta">
          <jh-tag size="small" class=${running ? 'brand' : (STANDING_APPEARANCE[ing.standing] ?? 'neutral')} label=${running ? 'judgement' : ing.standing}></jh-tag>
        </div>
        <dl class="facts" style="margin-top: var(--jh-space-400)">
          <dt>Ingress</dt><dd><box-hash label="ingress id" .value=${ing.id} chars="24"></box-hash></dd>
          <dt>Where</dt><dd>${place(ing) || html`<span class="muted">—</span>`}${ing.requester ? html` · requested by ${ing.requester}` : nothing}</dd>
          <dt>Source</dt><dd>${source(ing) || html`<span class="muted">not recorded</span>`}</dd>
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

/**
 * Origins: the audit story issue #187 asked for — every origination under a
 * delivery key, newest first, told the way the chain itself would tell it.
 * Every FACT line (an ingress, a plan, a build, a gate verdict, a rebase, a
 * landing, a close) carries its own actor and cites the chain record it came
 * from — tenant, scope, seq — so a reader can walk the same coordinates in
 * the export and find the identical record. Every DERIVED line (the totals)
 * says so and names the rows it folds. No diagram is drawn here: per
 * docs/PLAN.md §6 a graph layout is computed in Go and shipped as
 * coordinates, and this page draws only what /v1/origins/{key} sends —
 * nothing is counted or summed a second time in the browser.
 */
export class BoxOrigins extends LitElement {
  static properties = { originKey: { attribute: false } };
  static styles = [base, facts, css`
    :host { display: grid; gap: var(--jh-space-400); }
    .story + .story { margin-top: var(--jh-space-400); padding-top: var(--jh-space-400); border-top: 1px solid var(--jh-color-divider-secondary); }
    .story h2 { margin: 0 0 var(--jh-space-300); font: var(--jh-font-heading-medium-4); overflow-wrap: anywhere; }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; margin-bottom: var(--jh-space-200); }
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
    section.block { margin-top: var(--jh-space-400); }
    section.block h3 { margin: 0 0 var(--jh-space-200); font: var(--jh-font-body-md-semibold); }
    .block h4 { margin: var(--jh-space-300) 0 var(--jh-space-100); font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    .fact { padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    .fact .flabel { font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); margin-right: var(--jh-space-200); }
    .fact .fcontent { overflow-wrap: anywhere; }
    .fact .fcite { display: block; margin-top: 2px; font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .derived { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .tag-derived { font-weight: 700; letter-spacing: 0.04em; color: var(--jh-color-content-brand-enabled); }
    ol.nodes, ol.attempts, ol.sessions { list-style: none; margin: 0; padding: 0; }
    ol.nodes li { padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); overflow-wrap: anywhere; }
    .unit { margin-top: var(--jh-space-300); padding: var(--jh-space-300); border: 1px solid var(--jh-color-divider-secondary);
            border-radius: var(--jh-border-radius-200); }
    .unit h3 a { overflow-wrap: anywhere; }
    .attempt { padding: var(--jh-space-200) 0; border-bottom: 1px dashed var(--jh-color-divider-secondary); }
    .attempt .ahdr { font: var(--jh-font-body-md-semibold); margin-bottom: var(--jh-space-100); }
    .session-line { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: baseline;
                    font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled);
                    padding: var(--jh-space-100) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    .session-line > * { min-width: 0; overflow-wrap: anywhere; }
    .session-line .running { color: var(--jh-color-content-positive-enabled); font-weight: 600; }
    .actions { margin-bottom: var(--jh-space-400); }
  `];

  _task = new Task(this, {
    task: async ([key], { signal }) => (key ? getOrigins(key, signal) : []),
    args: () => [this.originKey],
  });

  // Every FACT line: the label, the content it asserts, and — always — the
  // actor and chain coordinates (tenant, scope, seq) it came from, exactly as
  // the API states them. `f` may be a FactRef itself or anything embedding
  // one; absence renders as "unknown actor" rather than a blank line, since a
  // fact with nobody attached to it is itself worth seeing.
  _fact(label, content, f) {
    return html`
      <div class="fact">
        <div class="fcontent"><span class="flabel">${label}</span>${content}</div>
        <div class="fcite">${f?.actor || 'unknown actor'} · ${factCite(f)} · <box-when .at=${f?.at}></box-when></div>
      </div>`;
  }

  _commit(sha, label = 'commit') {
    return sha ? html`<box-hash label=${label} .value=${sha} href=${'/provenance/' + sha}></box-hash>` : html`<span class="muted">none</span>`;
  }

  _gate(label, g) {
    if (!g) {
      return html`<div class="fact"><div class="fcontent"><span class="flabel">${label}</span><span class="muted">not judged yet</span></div></div>`;
    }
    const decision = g.decision
      ? html`<jh-tag size="small" class=${g.decision === 'pass' ? 'positive' : 'negative'} label=${g.decision}></jh-tag>`
      : html`<span class="muted">no verdict recorded</span>`;
    const carried = g.carried
      ? html` <span class="helper">carried forward${g.reviewed ? html` from the review of ${this._commit(g.reviewed)}` : nothing}${g.original ? ` (record ${g.original})` : ''}${g.patch_id ? ` · patch ${g.patch_id}` : ''}</span>`
      : nothing;
    return this._fact(label, html`${decision} on ${this._commit(g.sha)}${g.reason ? html` — ${g.reason}` : nothing}${carried}`, g);
  }

  _attempt(a) {
    return html`
      <li class="attempt">
        <div class="ahdr">Attempt ${a.n}</div>
        ${a.built
          ? this._fact('Built', html`${this._commit(a.built.sha)}${a.built.squashed_from ? html` · squashed from ${this._commit(a.built.squashed_from)}` : nothing}`, a.built)
          : html`<div class="fact"><div class="fcontent"><span class="flabel">Built</span><span class="muted">not built yet</span></div></div>`}
        ${this._gate('Check', a.check)}
        ${this._gate('Review', a.review)}
      </li>`;
  }

  _session(s) {
    return html`
      <li class="session-line">
        <jh-tag size="small" class=${ROLE_APPEARANCE[s.role] ?? 'neutral'} label=${s.role || 'agent'}></jh-tag>
        ${s.attempt ? html`<span>attempt ${s.attempt}${s.of ? ` of ${s.of}` : ''}</span>` : nothing}
        ${s.seat ? html`<span>seat ${s.seat}</span>` : nothing}
        ${s.model ? html`<span>${s.model}</span>` : nothing}
        <span><box-hash label="agentref" .value=${s.agentref}></box-hash></span>
        ${s.session ? html`<span>session <box-hash label="ACP session" .value=${s.session}></box-hash></span>` : nothing}
        ${s.continues ? html`<span>continues <box-hash label="ACP session" .value=${s.continues}></box-hash></span>` : nothing}
        ${s.sha ? html`<span>${this._commit(s.sha)}</span>` : nothing}
        ${s.started ? html`<span>started <box-when .at=${s.started}></box-when></span>` : nothing}
        ${s.finished
          ? html`<span>ended <box-when .at=${s.ended}></box-when> · ${fmtTokens(s.tokens)} tokens · ${fmtCost(s.cost_usd)}</span>`
          : html`<span class="running">running — cost unknown until it finishes</span>`}
        ${s.decision ? html`<span><jh-tag size="small" class=${s.decision === 'pass' ? 'positive' : 'negative'} label=${s.decision}></jh-tag>${s.reason ? html` ${firstLine(s.reason)}` : nothing}</span>` : nothing}
      </li>`;
  }

  _rebase(r) {
    if (r.kind === 'rebase-refused') {
      return this._fact('Rebase refused', html`onto ${this._commit(r.onto)}${r.reason ? html` — ${r.reason}` : nothing}`, r);
    }
    return this._fact('Rebased', html`${this._commit(r.was)} carried from ${this._commit(r.from)} onto ${this._commit(r.onto)}`, r);
  }

  _landing(l) {
    return this._fact('Landed', html`${l.repo}${l.branch ? ` / ${l.branch}` : ''}: ${this._commit(l.was)} → ${this._commit(l.now)}`, l);
  }

  _pr(p) {
    return this._fact('Pull request', html`<a href=${p.pr_url} target="_blank" rel="noopener">${p.pr_url}</a>${p.issue ? html` for issue ${p.issue}` : nothing}${p.completes_plan ? html` · <jh-tag size="small" class="positive" label="completes plan"></jh-tag>` : nothing}${p.note ? html` — ${p.note}` : nothing}`, p);
  }

  _landRefusal(r) {
    const label = r.kind === 'land-conflict' ? 'Land conflict' : 'Land refused';
    const content = r.kind === 'land-conflict'
      ? html`${r.repo}${r.branch ? ` / ${r.branch}` : nothing}${r.expected_base ? html` — expected base ${this._commit(r.expected_base)}` : nothing}`
      : html`${r.repo}${r.reason ? ` — ${r.reason}` : ''}`;
    return this._fact(label, content, r);
  }

  _close(u) {
    if (!u.closed) {
      return html`<div class="fact"><div class="fcontent"><span class="flabel">Close</span><span class="muted">no close record — not reported closed</span></div></div>`;
    }
    const c = u.closed;
    return this._fact('Closed', html`issue ${c.issue || '—'}${c.commit ? html` at ${this._commit(c.commit)}` : nothing}${c.pr_url ? html` · <a href=${c.pr_url} target="_blank" rel="noopener">${c.pr_url}</a>` : nothing}${c.closed_by ? html` · by ${c.closed_by}` : nothing}`, c);
  }

  _unit(u) {
    return html`
      <div class="unit">
        <h3><a href=${'/items/' + u.item_id}>${u.node_id}</a>${u.summary ? html` — ${u.summary}` : nothing}</h3>
        <div class="meta">
          <span class="helper">${u.sections?.length ? `sections: ${u.sections.join(', ')}` : 'whole repository'}</span>
          ${u.depends_on?.length ? html`<span class="helper">depends on ${u.depends_on.join(', ')}</span>` : nothing}
        </div>
        <section class="block">
          <h4>Attempts (${u.attempts?.length ?? 0})</h4>
          ${u.attempts?.length
            ? html`<ol class="attempts" aria-label=${`Attempts for ${u.node_id}`}>${u.attempts.map((a) => this._attempt(a))}</ol>`
            : html`<p class="empty">No attempts recorded.</p>`}
        </section>
        <section class="block">
          <h4>Sessions (${u.sessions?.length ?? 0})</h4>
          ${u.sessions?.length
            ? html`<ol class="sessions" aria-label=${`Agent sessions for ${u.node_id}`}>${u.sessions.map((s) => this._session(s))}</ol>`
            : html`<p class="empty">No agent sessions recorded.</p>`}
        </section>
        ${u.rebases?.length ? html`
          <section class="block">
            <h4>Rebases (${u.rebases.length})</h4>
            ${u.rebases.map((r) => this._rebase(r))}
          </section>` : nothing}
        <section class="block">
          <h4>Landing</h4>
          ${u.landings?.length ? u.landings.map((l) => this._landing(l)) : html`<p class="empty">Not landed.</p>`}
          ${u.prs?.map((p) => this._pr(p)) ?? nothing}
          ${u.land_refusals?.map((r) => this._landRefusal(r)) ?? nothing}
        </section>
        <section class="block">
          <h4>Close</h4>
          ${this._close(u)}
        </section>
      </div>`;
  }

  _node(n) {
    return html`
      <li>
        <code>${n.id}</code> — ${title(n)}
        <div class="helper">
          ${n.repo ? `repo ${n.repo} · ` : ''}sections: ${n.sections?.length ? n.sections.join(', ') : 'whole repository'}
          ${n.depends_on?.length ? html` · depends on ${n.depends_on.map((d) => (n.edge_reasons?.[d] ? `${d} (${n.edge_reasons[d]})` : d)).join(', ')}` : nothing}
        </div>
      </li>`;
  }

  _plan(pf) {
    const p = pf.plan;
    return html`
      <div>
        ${this._fact('Plan sealed', html`
          by ${p.decomposer}
          <jh-tag size="small" class=${p.replayable ? 'positive' : 'neutral'} label=${p.replayable ? 'replayable' : 'judgement'}></jh-tag>
          ${p.ref ? html` · <a href=${'/plans/' + p.ref}>full plan</a>` : nothing}
          ${!p.replayable && p.agentref ? html` · agent <box-hash label="agentref" .value=${p.agentref}></box-hash>` : nothing}`, pf)}
        <ol class="nodes" aria-label="Plan nodes, in order">${(p.nodes ?? []).map((n) => this._node(n))}</ol>
      </div>`;
  }

  _planEvent(e) {
    return this._fact(e.kind, e.reason ? html`${e.reason}` : html`<span class="muted">no reason recorded</span>`, e);
  }

  _judgement(story) {
    return html`
      <section class="block">
        <h3>Judgement</h3>
        ${story.plans?.length ? story.plans.map((pf) => this._plan(pf)) : html`<p class="empty">No plan recorded yet.</p>`}
        ${story.plan_events?.length ? html`
          <div>
            <h4>Plan events</h4>
            ${story.plan_events.map((e) => this._planEvent(e))}
          </div>` : nothing}
        <div>
          <h4>Planner sessions (${story.sessions?.length ?? 0})</h4>
          ${story.sessions?.length
            ? html`<ol class="sessions">${story.sessions.map((s) => this._session(s))}</ol>`
            : html`<p class="empty">No planner sessions recorded.</p>`}
        </div>
      </section>`;
  }

  _ingressSection(story) {
    const ing = story.ingress;
    return html`
      <section class="block">
        <h3>Ingress</h3>
        ${this._fact('Delivered', html`
          <a href=${'/ingress/' + story.ingress_id}>${story.ingress_id}</a>
          ${story.route ? ` on ${story.route}` : ''}
          ${story.key ? ` for ${story.key}` : ''}
          ${ing.requester ? ` by ${ing.requester}` : ''}
          ${story.box || story.repo ? ` · ${[story.box, story.repo].filter(Boolean).join(' / ')}` : ''}
          ${ing.title ? html`<div>${ing.title}</div>` : nothing}
          ${ing.note ? html`<div class="helper">${ing.note}</div>` : nothing}`, ing)}
      </section>`;
  }

  _totals(t) {
    const o = originTotals(t);
    return html`
      <section class="block">
        <h4>Totals</h4>
        <p class="derived">
          <span class="tag-derived">DERIVED</span>
          ${o.attempts} attempt(s) ·
          ${o.tokens === 'unknown'
            ? html`cost unknown — 0 of ${o.sessions} session(s) covered`
            : html`${o.tokens} tokens · ${o.cost}${o.wallSec != null ? ` · ${o.wallSec}s wall` : ''} — ${o.covered} of ${o.sessions} session(s) covered${o.partial ? ', partial sum' : ''}`}
          <span class="helper">folded from the planner's and every unit's agent-session rows above</span>
        </p>
      </section>`;
  }

  _story(story) {
    return html`
      <div class="story">
        <h2>${story.ingress_id}${story.key && story.key !== this.originKey ? html` <span class="helper">(${story.key})</span>` : nothing}</h2>
        ${this._ingressSection(story)}
        ${this._judgement(story)}
        <section class="block">
          <h3>Units (${story.units?.length ?? 0})</h3>
          ${story.units?.length ? story.units.map((u) => this._unit(u)) : html`<p class="empty">No units decomposed yet.</p>`}
        </section>
        ${this._totals(story.totals)}
      </div>`;
  }

  render() {
    return html`
      <jh-card header-title="Origins" header-subtitle=${this.originKey} padding="medium">
        ${this._task.render({
          pending: () => loading('origins'),
          error: failed,
          complete: (stories) => (stories?.length ?? 0) === 0
            ? html`<p class="empty">Nothing is recorded under this key. Either it never came through this factory, or it has not been delivered yet.</p>`
            : html`
              <div class="actions">
                <jh-button appearance="secondary" size="small" label="Evidence artifact"
                  href=${originsExportURL(this.originKey)} target="_blank"></jh-button>
                <span class="helper">Every origination under this key, as one self-contained file — the same record this page renders, verifiable offline.</span>
              </div>
              ${stories.map((s) => this._story(s))}`,
        })}
      </jh-card>`;
  }
}
customElements.define('box-origins', BoxOrigins);

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
    .meta > * { min-width: 0; overflow-wrap: anywhere; }
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
          <dt>Depends on</dt><dd>${n.depends_on?.length
            ? n.depends_on.map((depId) => n.edge_reasons?.[depId] ? `${depId} — ${n.edge_reasons[depId]}` : depId).join(', ')
            : html`<span class="muted">nothing</span>`}</dd>
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
          <p class="fanout">${plan.fan_out} unit(s)${plan.width && plan.depth ? html` · width ${plan.width} · depth ${plan.depth}` : nothing}</p>
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
