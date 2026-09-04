// Shared pieces every view leans on: a hash that reads as an identity and
// copies as a value, a timestamp that says how long ago, an action that asks
// for its reason before it becomes a record, and the toast that reports what
// happened. The focus ring and the tag colours are the design system's own,
// reached through its tokens and style hooks.
import { LitElement, html, css, nothing } from 'lit';
import { when } from './format.js';
import { icon } from './icons.js';

/** base is the styling every view shares: links, custom controls, the
 *  visually-hidden helper, and jh-tag's colour hooks by class. */
export const base = css`
  a { color: var(--jh-color-content-brand-enabled); text-decoration: underline; text-underline-offset: 2px; }
  a:hover { color: var(--jh-color-content-brand-hover); }
  a:focus-visible, button:focus-visible, [tabindex]:focus-visible {
    outline: none; box-shadow: var(--jh-shadow-focus); border-radius: var(--jh-border-radius-100);
  }
  .vh { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
  .muted { color: var(--jh-color-content-secondary-enabled); }
  .helper { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
  .micro { font: var(--jh-font-micro-medium); color: var(--jh-color-content-secondary-enabled); text-transform: uppercase; letter-spacing: .04em; }
  code, .mono { font: var(--jh-font-code-regular-2); font-size: .85em; }
  .empty { color: var(--jh-color-content-secondary-enabled); margin: 0; }
  /* A tinted container pairs with its own content colour: red-100 under
     red-600 in the light theme, red-800 under red-300 in the dark. The
     "on-" colours are for solid surfaces and vanish on a tint. */
  jh-tag { --jh-tag-color-border-enabled: transparent; }
  jh-tag.positive {
    --jh-tag-color-background-enabled: var(--jh-color-container-positive-enabled);
    --jh-tag-color-text-enabled: var(--jh-color-content-positive-enabled);
  }
  jh-tag.negative {
    --jh-tag-color-background-enabled: var(--jh-color-container-negative-enabled);
    --jh-tag-color-text-enabled: var(--jh-color-content-negative-enabled);
  }
  jh-tag.brand {
    --jh-tag-color-background-enabled: var(--jh-color-container-brand-enabled);
    --jh-tag-color-text-enabled: var(--jh-color-content-brand-enabled);
  }
  jh-notification a { color: inherit; text-decoration: underline; }
  jh-notification { display: block; margin-top: var(--jh-space-300); }
  .actions { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; }
`;

const escapeHTML = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

/** toast reports an outcome through jh-toast-controller's live region. The
 *  controller sets innerHTML from the text, so anything that came from a
 *  server or an operator is escaped first. */
export function toast(text, appearance = 'neutral', timeout = 6000) {
  window.dispatchEvent(new CustomEvent('jh-create-toast', {
    detail: { text: escapeHTML(text), appearance, timeout, dismissButtonAccessibleLabel: 'Dismiss' },
  }));
}

/** focusInto puts focus on a jh-input's native field. */
export function focusInto(el) {
  const inner = el?.shadowRoot?.querySelector('input, textarea');
  (inner ?? el)?.focus?.();
}

/**
 * box-hash: an object id shown the way git shows one — the first characters,
 * which are the identity a person recognises — with the full value one click
 * away. The full 40 or 64 characters were the whole of what several views
 * showed, and nobody reads those; they compare prefixes and copy the rest.
 */
export class BoxHash extends LitElement {
  static properties = { value: {}, label: {}, href: {}, chars: { type: Number } };
  static styles = [base, css`
    :host { display: inline-flex; align-items: center; gap: var(--jh-space-100); vertical-align: middle; }
    code { padding: 1px var(--jh-space-100); border-radius: var(--jh-border-radius-100);
           background: var(--jh-color-container-secondary-enabled); }
    a code { color: inherit; }
    button { font: var(--jh-font-micro-medium); cursor: pointer; padding: 2px var(--jh-space-200);
             border: 1px solid var(--jh-color-control-enabled); border-radius: var(--jh-border-radius-pill);
             background: transparent; color: var(--jh-color-content-secondary-enabled); }
    button:hover { color: var(--jh-color-content-primary-enabled); border-color: var(--jh-color-control-hover); }
  `];
  constructor() {
    super();
    this.chars = 10;
    this.label = 'id';
  }
  async _copy() {
    try {
      await navigator.clipboard.writeText(this.value);
      toast(`Copied ${this.label} ${this.value.slice(0, this.chars)}…`, 'positive', 3000);
    } catch {
      toast(`Could not copy — select the ${this.label} and copy it by hand`, 'negative');
    }
  }
  render() {
    const v = String(this.value ?? '');
    if (!v) return nothing;
    const shortForm = v.length > this.chars + 2 ? v.slice(0, this.chars) : v;
    const code = html`<code title=${v}>${shortForm}</code>`;
    return html`
      ${this.href ? html`<a href=${this.href} aria-label=${`${this.label} ${shortForm}`}>${code}</a>` : code}
      ${v.length > shortForm.length
        ? html`<button type="button" @click=${this._copy} aria-label=${`Copy the full ${this.label}`}>Copy</button>`
        : nothing}`;
  }
}
customElements.define('box-hash', BoxHash);

/** box-when: a timestamp as how long ago, with the absolute form on hover
 *  and in the datetime attribute for anyone parsing the page. */
export class BoxWhen extends LitElement {
  static properties = { at: {}, abs: { type: Boolean } };
  static styles = css`:host { display: inline; } time { font-variant-numeric: tabular-nums; }`;
  render() {
    const w = when(this.at);
    if (!w.iso) return html`${w.rel}`;
    return html`<time datetime=${w.iso} title=${w.abs}>${this.abs ? w.abs : w.rel}</time>`;
  }
}
customElements.define('box-when', BoxWhen);

/**
 * box-reason: a control that asks for its reason before it acts.
 *
 * Every verb on this surface becomes a record, and a record needs one — the
 * API refuses a stop, a retry or a resolution without it. The first shell put
 * one bare "Reason" box beside Stop and Resume and disabled both until it was
 * filled, which read as two broken buttons. Here the reason is asked for by
 * the action that needs it, right where it was pressed, and the record it
 * becomes is spelled out.
 *
 * `action(reason)` performs the verb and resolves when the server has
 * accepted it; the host hears `box-done` on success.
 */
export class BoxReason extends LitElement {
  static properties = {
    label: {}, confirm: {}, appearance: {}, size: {}, prompt: {}, glyph: {},
    // floating puts the form over what follows instead of pushing it: for
    // the strip, where a form in the flow made the row jump. Cards keep the
    // inline form, because a card clips what overflows it.
    floating: { type: Boolean },
    action: { attribute: false },
    _open: { state: true }, _reason: { state: true }, _pending: { state: true }, _error: { state: true },
  };
  static styles = [base, css`
    :host { display: inline-block; position: relative; }
    form { display: grid; gap: var(--jh-space-200); margin-top: var(--jh-space-200);
           padding: var(--jh-space-300); max-width: 420px;
           background: var(--jh-color-container-primary-enabled);
           border: 1px solid var(--jh-color-divider-primary);
           border-radius: var(--jh-border-radius-200); box-shadow: var(--jh-shadow-200); }
    :host([floating]) form { position: absolute; top: calc(100% + var(--jh-space-100)); left: 0; z-index: var(--jh-z-index-positive-300);
                            width: max(320px, 100%); margin-top: 0; }
    .row { display: flex; gap: var(--jh-space-200); }
    jh-notification { margin: 0; }
  `];
  constructor() {
    super();
    this.appearance = 'secondary';
    this.size = 'small';
    this._open = false;
    this._reason = '';
    this._pending = false;
    this._error = '';
  }
  async _toggle() {
    this._open = !this._open;
    this._error = '';
    if (this._open) {
      await this.updateComplete;
      focusInto(this.renderRoot.querySelector('jh-input'));
    }
  }
  _cancel() {
    this._open = false;
    this._reason = '';
    this._error = '';
    this.renderRoot.querySelector('jh-button')?.focus();
  }
  async _submit(e) {
    e.preventDefault();
    const reason = this._reason.trim();
    if (!reason || this._pending) return;
    this._pending = true;
    this._error = '';
    try {
      const result = await this.action(reason);
      this._open = false;
      this._reason = '';
      this.dispatchEvent(new CustomEvent('box-done', { detail: { reason, result }, bubbles: true, composed: true }));
    } catch (err) {
      this._error = err.message;
    } finally {
      this._pending = false;
    }
  }
  render() {
    return html`
      <jh-button appearance=${this.appearance} size=${this.size} label=${this.label}
        aria-expanded=${this._open ? 'true' : 'false'} @click=${this._toggle}>
        ${this.glyph ? html`<span slot="jh-button-icon-left">${icon(this.glyph, 'x-small')}</span>` : nothing}
      </jh-button>
      ${this._open ? html`
        <form role="group" aria-label=${this.prompt ?? this.label} @submit=${this._submit}
          @keydown=${(e) => { if (e.key === 'Escape') this._cancel(); }}>
          <jh-input size="small" label="Reason" required
            helper-text="Becomes the record: who did this, when, and why"
            .value=${this._reason}
            @jh-change=${(e) => { this._reason = e.target.value ?? ''; }}
            @input=${(e) => { this._reason = e.composedPath()[0].value ?? this._reason; }}></jh-input>
          <div class="row">
            <jh-button submit size="small" label=${this.confirm ?? this.label}
              appearance=${this.appearance === 'danger' ? 'danger' : 'primary'}
              ?pending=${this._pending} ?disabled=${!this._reason.trim()}></jh-button>
            <jh-button appearance="tertiary" size="small" label="Cancel" @click=${this._cancel}></jh-button>
          </div>
          ${this._error ? html`<jh-notification appearance="negative" type="alert" hide-dismiss-button>${this._error}</jh-notification>` : nothing}
        </form>` : nothing}`;
  }
}
customElements.define('box-reason', BoxReason);
