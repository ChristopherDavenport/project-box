import { getActivity } from './api.js';

// ActivityController: a reactive controller owning the SSE subscription —
// lifecycle-aware setup and teardown, per the Lit controller pattern for
// ongoing resources.
export class ActivityController {
  events = [];
  connected = false;

  constructor(host, { limit = 100 } = {}) {
    this._host = host;
    this._limit = limit;
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
      // New reference, never a mutation: Lit change detection is `!==`.
      this.events = [...this.events, JSON.parse(e.data)].slice(-this._limit);
      this._host.requestUpdate();
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
