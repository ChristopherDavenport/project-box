import { LitElement, html, css, nothing } from 'lit';
import { Task } from '@lit/task';
import { getStatus, getExceptions, getItem, getItemEvidence, getProvenance, openIngress } from './api.js';
import { ActivityController } from './sse.js';

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
  `;

  _status = new Task(this, {
    task: async (_, { signal }) => getStatus(signal),
    args: () => [],
  });
  _activity = new ActivityController(this);

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
              <div class="stat"><div class="n">${s.stopped ? 'stopped' : 'running'}</div><div class="l">claims</div></div>
              <div class="stat"><div class="n">${s.routes.length}</div><div class="l">routes</div></div>
            </div>`,
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

/** Exceptions: what still needs a person. */
export class BoxExceptions extends LitElement {
  static styles = css`:host { display: block; } .empty { color: var(--jh-color-content-secondary-enabled); }`;
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
                  <jh-list-item primary-text=${it.id} secondary-text=${it.note} show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${it.state}></jh-tag>
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
  `;
  _task = new Task(this, {
    task: async ([id], { signal }) => {
      const [item, evidence] = await Promise.all([getItem(id, signal), getItemEvidence(id, signal)]);
      return { item, evidence };
    },
    args: () => [this.itemId],
  });

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
        <jh-card header-title="Evidence" header-subtitle="The item's chain, in order" padding="medium">
          <jh-list-group accessible-label="Evidence records">
            ${(evidence ?? []).map((r) => html`
              <jh-list-item primary-text=${r.Kind} secondary-text=${r.Subject}
                primary-metadata=${'seq ' + r.Seq} secondary-metadata=${r.Actor} show-divider></jh-list-item>`)}
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
