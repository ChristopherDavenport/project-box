import { LitElement, html, css, nothing } from 'lit';
import { Task } from '@lit/task';
import { getStatus, getItems, getExceptions, getItem, getItemEvidence, getProvenance, openIngress } from './api.js';
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
  pending: ['pending'],
  claimed: ['claimed'],
  waiting: ['waiting'],
  handedoff: ['handedoff'],
  parked: ['parked'],
  done: ['done'],
  all: [],
};

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
 * Work: every unit and why it is where it is.
 *
 * The room could show what was stuck and what had landed, and nothing in
 * between — `pending=14` was the whole view into the queue. The notes were
 * always written ("branch main is leased", a gate's reasons) and never shown.
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
    .note { white-space: pre-wrap; font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    .blocked { font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;
  static properties = { _filter: { state: true } };

  constructor() {
    super();
    this._filter = 'live';
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

  render() {
    return html`
      <jh-card header-title="Work" header-subtitle="Every unit, and why it is where it is" padding="medium">
        <div class="filters" role="group" aria-label="Filter by state">
          ${Object.keys(FILTERS).map((f) => html`
            <button type="button" aria-pressed=${this._filter === f ? 'true' : 'false'}
              @click=${() => { this._filter = f; }}>${f}</button>`)}
        </div>
        <div class="live">${this._activity.connected ? 'live' : 'reconnecting…'}</div>
        ${this._task.render({
          pending: () => html`<jh-progress accessible-label="Loading work"></jh-progress>`,
          error: (e) => html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${e.message}</jh-notification>`,
          complete: (items) => (items?.length ?? 0) === 0
            ? html`<p class="empty">Nothing in this state.</p>`
            : html`<jh-list-group accessible-label="Work items">
                ${items.map((e) => html`
                  <jh-list-item
                    primary-text=${e.task || e.id}
                    secondary-text=${origin(e)}
                    primary-metadata=${age(e.age_sec)}
                    secondary-metadata=${e.stage ? `${e.stage} · ${age(e.in_state_sec)}` : ''}
                    show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${e.state}></jh-tag>
                    <div slot="jh-list-item-content">
                      ${e.note ? html`<div class="note">${e.note}</div>` : nothing}
                      ${e.blocked_by?.length
                        ? html`<div class="blocked">blocked by ${e.blocked_by.join(', ')}</div>`
                        : nothing}
                    </div>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${'/items/' + e.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`,
        })}
      </jh-card>`;
  }
}
customElements.define('box-work', BoxWork);

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
