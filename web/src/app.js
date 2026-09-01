// The Box control room: Lit + @lit-labs/router (Labs, pre-1.0) + the Jack
// Henry Design System. Served by boxd itself; talks to /v1 on the same
// origin.
import { LitElement, html, css } from 'lit';
import { Router } from '@lit-labs/router';
import { bootstrapToken, stop, resume } from './api.js';

// JH components used anywhere in the app — imported once, for their side
// effect of defining the custom elements.
import '@jack-henry/jh-ui/components/button/button.js';
import '@jack-henry/jh-ui/components/card/card.js';
import '@jack-henry/jh-ui/components/tag/tag.js';
import '@jack-henry/jh-ui/components/input/input.js';
import '@jack-henry/jh-ui/components/radio-group/radio-group.js';
import '@jack-henry/jh-ui/components/radio/radio.js';
import '@jack-henry/jh-ui/components/notification/notification.js';
import '@jack-henry/jh-ui/components/list-group/list-group.js';
import '@jack-henry/jh-ui/components/list-item/list-item.js';
import '@jack-henry/jh-ui/components/divider/divider.js';
import '@jack-henry/jh-ui/components/progress/progress.js';

import './pages.js';

bootstrapToken();

export class BoxApp extends LitElement {
  static styles = css`
    :host { display: block; max-width: 960px; margin: 0 auto; padding: 16px; }
    header { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
    h1 { font-size: 1.2rem; margin: 0; }
    nav { display: flex; gap: 4px; flex: 1; }
    nav a { text-decoration: none; padding: 6px 10px; border-radius: 6px;
            color: var(--jh-color-content-primary-enabled); font-size: .9rem; }
    nav a.active { background: var(--jh-color-container-secondary-enabled); font-weight: 600; }
    .controls { display: flex; gap: 8px; align-items: end; }
    main { display: block; }
  `;
  static properties = { _reason: { state: true }, _controlError: { state: true } };

  constructor() {
    super();
    this._reason = '';
    this._controlError = '';
  }

  _router = new Router(this, [
    { path: '/', render: () => html`<box-dashboard></box-dashboard>` },
    { path: '/exceptions', render: () => html`<box-exceptions></box-exceptions>` },
    { path: '/items/:id', render: ({ id }) => html`<box-item .itemId=${id}></box-item>` },
    { path: '/provenance/:sha', render: ({ sha }) => html`<box-provenance .sha=${sha}></box-provenance>` },
    { path: '/submit', render: () => html`<box-submit></box-submit>` },
  ], { fallback: { render: () => html`<jh-notification appearance="neutral" type="banner" hide-dismiss-button>No such page.</jh-notification>` } });

  _active(href) {
    return location.pathname === href ? 'active' : '';
  }

  async _control(action) {
    this._controlError = '';
    try {
      await (action === 'stop' ? stop(this._reason) : resume(this._reason));
      this._reason = '';
      // The dashboard re-fetches on its own next visit; nudge a render so
      // the nav reflects immediately.
      this.requestUpdate();
    } catch (e) {
      this._controlError = e.message;
    }
  }

  render() {
    return html`
      <header>
        <h1>Box Control Room</h1>
        <nav>
          <a href="/" class=${this._active('/')}>Dashboard</a>
          <a href="/exceptions" class=${this._active('/exceptions')}>Exceptions</a>
          <a href="/submit" class=${this._active('/submit')}>Submit</a>
        </nav>
        <div class="controls">
          <jh-input size="small" label="Reason" .value=${this._reason}
            @jh-change=${(e) => { this._reason = e.target.value; }}></jh-input>
          <jh-button appearance="danger" size="small" label="Stop"
            ?disabled=${!this._reason} @click=${() => this._control('stop')}></jh-button>
          <jh-button appearance="secondary" size="small" label="Resume"
            ?disabled=${!this._reason} @click=${() => this._control('resume')}></jh-button>
        </div>
      </header>
      ${this._controlError ? html`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${this._controlError}</jh-notification>` : ''}
      <main>${this._router.outlet()}</main>
    `;
  }
}
customElements.define('box-app', BoxApp);
