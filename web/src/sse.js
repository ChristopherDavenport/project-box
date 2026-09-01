import { getActivity } from './api.js';

// ActivityController: a reactive controller owning the SSE subscription —
// lifecycle-aware setup and teardown, per the Lit controller pattern for
// ongoing resources.
export class ActivityController {
  events = [];
  connected = false;

  // onEvent fires for every activity record as it lands, so a view that
  // renders derived state — the queue, one item's timeline — can refresh
  // itself instead of making the operator reload the page to see the factory
  // move.
  constructor(host, { limit = 100, onEvent } = {}) {
    this._host = host;
    this._limit = limit;
    this._onEvent = onEvent;
    host.addController(this);
  }

  hostConnected() {
    getActivity().then((initial) => {
      this.events = (initial ?? []).slice(-this._limit);
      this._host.requestUpdate();
    }).catch(() => { /* the stream below still tells the story */ });
    this._es = new EventSource('/v1/events');
    this._es.addEventListener('hello', () => {
      this.connected = true;
      this._host.requestUpdate();
    });
    this._es.addEventListener('activity', (e) => {
      const a = JSON.parse(e.data);
      // New reference, never a mutation: Lit change detection is `!==`.
      this.events = [...this.events, a].slice(-this._limit);
      this._host.requestUpdate();
      this._onEvent?.(a);
    });
    this._es.onerror = () => {
      this.connected = false;
      this._host.requestUpdate();
    };
  }

  hostDisconnected() {
    this._es?.close();
    this._es = undefined;
  }
}
