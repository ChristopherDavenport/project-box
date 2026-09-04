// The Box control room: Lit + @lit-labs/router (Labs, pre-1.0) + the Jack
// Henry Design System. Served by boxd itself; talks to /v1 on the same
// origin.
//
// The shell is the part of the room that is on every page: where you are,
// where else you can go, whether the feed is live, whether claims are
// running and who last changed that, and which theme the tokens are in.
import { LitElement, html, css, nothing } from 'lit';
import { Router } from '@lit-labs/router';
import { bootstrapToken, getStatus, stop, resume } from './api.js';
import { ActivityController } from './sse.js';
import { THEMES, getTheme, setTheme } from './theme.js';
import { icon } from './icons.js';
import { base, toast } from './ui.js';
import { age } from './format.js';

// JH components used anywhere in the app — imported once, for their side
// effect of defining the custom elements.
import '@jack-henry/jh-ui/components/button/button.js';
import '@jack-henry/jh-ui/components/card/card.js';
import '@jack-henry/jh-ui/components/tag/tag.js';
import '@jack-henry/jh-ui/components/tag-group/tag-group.js';
import '@jack-henry/jh-ui/components/icon/icon.js';
import '@jack-henry/jh-ui/components/input/input.js';
import '@jack-henry/jh-ui/components/radio-group/radio-group.js';
import '@jack-henry/jh-ui/components/radio/radio.js';
import '@jack-henry/jh-ui/components/notification/notification.js';
import '@jack-henry/jh-ui/components/list-group/list-group.js';
import '@jack-henry/jh-ui/components/list-item/list-item.js';
import '@jack-henry/jh-ui/components/divider/divider.js';
import '@jack-henry/jh-ui/components/progress/progress.js';
import '@jack-henry/jh-ui/components/toast/toast.js';
import '@jack-henry/jh-ui/components/toast-controller/toast-controller.js';

import './pages.js';

bootstrapToken();

const NAV = [
  { href: '/', label: 'Dashboard' },
  { href: '/work', label: 'Work' },
  { href: '/sessions', label: 'Sessions' },
  { href: '/exceptions', label: 'Exceptions' },
  { href: '/submit', label: 'Submit' },
];

/**
 * box-theme: which token set the room is in. Three explicit choices rather
 * than a toggle, because "follow the desktop" is the setting most people
 * want and a two-state switch cannot express it.
 */
export class BoxTheme extends LitElement {
  static properties = { _theme: { state: true } };
  static styles = [base, css`
    .seg { display: inline-flex; border: 1px solid var(--jh-color-control-enabled);
           border-radius: var(--jh-border-radius-pill); overflow: hidden; }
    button { display: inline-flex; align-items: center; gap: var(--jh-space-100);
             font: var(--jh-font-helper-medium); cursor: pointer; padding: 4px var(--jh-space-300);
             border: 0; background: transparent; color: var(--jh-color-content-secondary-enabled); }
    button + button { border-left: 1px solid var(--jh-color-control-enabled); }
    button:hover { color: var(--jh-color-content-primary-enabled); background: var(--jh-color-container-secondary-hover); }
    button[aria-pressed="true"] { background: var(--jh-color-container-secondary-selected);
                                  color: var(--jh-color-content-primary-enabled); font-weight: 600; }
    button:focus-visible { border-radius: 0; position: relative; z-index: 1; }
    jh-icon { --jh-icon-color-fill: currentColor; }
    @media (max-width: 720px) { button span { display: none; } }
  `];
  constructor() {
    super();
    this._theme = getTheme();
    this._onTheme = (e) => { this._theme = e.detail.theme; };
  }
  connectedCallback() {
    super.connectedCallback();
    window.addEventListener('box-theme', this._onTheme);
  }
  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener('box-theme', this._onTheme);
  }
  render() {
    return html`
      <div class="seg" role="group" aria-label="Theme">
        ${THEMES.map((t) => html`
          <button type="button" aria-pressed=${this._theme === t.id ? 'true' : 'false'}
            title=${`${t.label} theme`} @click=${() => setTheme(t.id)}>
            ${icon(t.icon, 'x-small')}<span>${t.label}</span>
          </button>`)}
      </div>`;
  }
}
customElements.define('box-theme', BoxTheme);

/**
 * box-claims: the andon cord, with its record. One strip on every page that
 * says whether claims are running, who last changed that, when, and why —
 * and offers exactly the one action that applies, which asks for its reason
 * before it becomes the next record.
 *
 * The daemon's identity sits at the other end of the strip. Tenant, box,
 * runner, version and uptime were on /v1/status from the start and nowhere
 * on the page.
 */
export class BoxClaims extends LitElement {
  static properties = { _status: { state: true }, _error: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .strip { display: flex; align-items: center; gap: var(--jh-space-300); flex-wrap: wrap;
             padding: var(--jh-space-200) var(--jh-space-400);
             background: var(--jh-color-container-primary-enabled);
             border: 1px solid var(--jh-color-divider-secondary);
             border-radius: var(--jh-border-radius-300); }
    .state { display: flex; align-items: center; gap: var(--jh-space-300); flex: 1; min-width: 240px; flex-wrap: wrap; }
    .who { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); flex: 1 1 220px; }
    .who b { color: var(--jh-color-content-primary-enabled); font-weight: 600; }
    .id { display: flex; gap: var(--jh-space-300); flex-wrap: wrap; font: var(--jh-font-helper-regular);
          color: var(--jh-color-content-secondary-enabled); }
    .id b { color: var(--jh-color-content-primary-enabled); font-weight: 500; }
    .live { display: inline-flex; align-items: center; gap: var(--jh-space-100); font: var(--jh-font-helper-medium); }
    .live jh-icon { --jh-icon-color-fill: var(--jh-color-content-positive-enabled); }
    .live.down jh-icon { --jh-icon-color-fill: var(--jh-color-content-negative-enabled); }
  `];
  constructor() {
    super();
    this._status = null;
    this._error = '';
    // Stop and resume are records on the feed like everything else; a
    // change from another operator's window shows here without a reload.
    this._activity = new ActivityController(this, {
      limit: 1,
      onEvent: (a) => { if (a.scope === 'control/claims' || a.kind === 'config-reloaded') this._refresh(); },
    });
  }
  connectedCallback() {
    super.connectedCallback();
    this._refresh();
    this._onDone = () => this._refresh();
    window.addEventListener('box-done', this._onDone);
  }
  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this._timer);
    window.removeEventListener('box-done', this._onDone);
  }
  _refresh() {
    clearTimeout(this._timer);
    this._timer = setTimeout(async () => {
      try {
        this._status = await getStatus();
        this._error = '';
      } catch (e) {
        this._error = e.message;
      }
    }, 150);
  }
  render() {
    const s = this._status;
    const c = s?.claims;
    const up = this._activity.connected;
    return html`
      <div class="strip">
        <span class=${'live' + (up ? '' : ' down')} role="status">
          ${icon('dot', 'x-small')}${up ? 'Live' : 'Reconnecting…'}
        </span>
        <div class="state">
          ${!s ? html`<span class="who">${this._error || 'Reading the factory…'}</span>` : html`
            <jh-tag size="small" class=${c?.stopped ? 'negative' : 'positive'}
              label=${c?.stopped ? 'Claims stopped' : 'Claims running'}></jh-tag>
            ${c?.actor ? html`
              <span class="who">${c.stopped ? 'stopped' : 'resumed'} <box-when .at=${c.at}></box-when>
                by <b>${c.actor}</b>${c.reason ? html` — ${c.reason}` : nothing}</span>`
              : html`<span class="who">nobody has pulled the cord</span>`}
            ${c?.stopped
              ? html`<box-reason floating label="Resume claims" confirm="Resume" appearance="primary" glyph="play"
                  prompt="Resume claims" .action=${(r) => resume(r).then(() => toast('Claims resumed', 'positive'))}></box-reason>`
              : html`<box-reason floating label="Stop claims" confirm="Stop" appearance="danger" glyph="stop"
                  prompt="Stop claims" .action=${(r) => stop(r).then(() => toast('Claims stopped — in-flight runs settle, nothing new starts', 'negative'))}></box-reason>`}`}
        </div>
        ${s ? html`
          <div class="id" aria-label="Daemon">
            <span>tenant <b>${s.tenant}</b></span>
            <span>box <b>${s.box}</b></span>
            <span>runner <b>${s.runner}</b></span>
            <span>version <b>${s.version}</b></span>
            <span>up <b>${age(s.uptime_sec)}</b></span>
          </div>` : nothing}
      </div>`;
  }
}
customElements.define('box-claims', BoxClaims);

export class BoxApp extends LitElement {
  static styles = [base, css`
    :host { display: block; max-width: 1080px; margin: 0 auto; padding: var(--jh-space-400); }
    .skip { position: absolute; left: -999px; top: var(--jh-space-200); padding: var(--jh-space-200) var(--jh-space-300);
            background: var(--jh-color-container-primary-enabled); border-radius: var(--jh-border-radius-200);
            box-shadow: var(--jh-shadow-300); z-index: 10; }
    .skip:focus { left: var(--jh-space-400); }
    header { display: flex; align-items: center; gap: var(--jh-space-400); flex-wrap: wrap; margin-bottom: var(--jh-space-300); }
    h1 { font: var(--jh-font-heading-medium-4); margin: 0; }
    nav { display: flex; gap: var(--jh-space-100); flex: 1; flex-wrap: wrap; }
    nav a { text-decoration: none; padding: 6px var(--jh-space-300); border-radius: var(--jh-border-radius-pill);
            color: var(--jh-color-content-primary-enabled); font: var(--jh-font-body-medium-1); }
    nav a:hover { background: var(--jh-color-container-secondary-hover); }
    nav a[aria-current="page"] { background: var(--jh-color-container-secondary-selected); font-weight: 600; }
    main { display: block; margin-top: var(--jh-space-400); }
    main:focus { outline: none; }
  `];

  _router = new Router(this, [
    { path: '/', render: () => html`<box-dashboard></box-dashboard>` },
    { path: '/work', render: () => html`<box-work></box-work>` },
    { path: '/sessions', render: () => html`<box-sessions></box-sessions>` },
    { path: '/exceptions', render: () => html`<box-exceptions></box-exceptions>` },
    { path: '/items/:id', render: ({ id }) => html`<box-item .itemId=${id}></box-item>` },
    { path: '/provenance/:sha', render: ({ sha }) => html`<box-provenance .sha=${sha}></box-provenance>` },
    { path: '/submit', render: () => html`<box-submit></box-submit>` },
  ], {
    fallback: {
      render: () => html`
        <jh-card header-title="No such page" padding="medium">
          <p class="empty">Nothing lives at <code>${location.pathname}</code>. <a href="/">Back to the dashboard</a>.</p>
        </jh-card>`,
    },
  });

  _current(href) {
    return location.pathname === href ? 'page' : nothing;
  }

  render() {
    return html`
      <a class="skip" href="#main">Skip to content</a>
      <header>
        <h1>Box control room</h1>
        <nav aria-label="Primary">
          ${NAV.map((n) => html`<a href=${n.href} aria-current=${this._current(n.href)}>${n.label}</a>`)}
        </nav>
        <box-theme></box-theme>
      </header>
      <box-claims></box-claims>
      <main id="main" tabindex="-1">${this._router.outlet()}</main>
      <jh-toast-controller></jh-toast-controller>
    `;
  }
}
customElements.define('box-app', BoxApp);
