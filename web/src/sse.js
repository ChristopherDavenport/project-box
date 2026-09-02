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
  //
  // onSession fires for the other half: the frames a running agent is
  // producing right now. They arrive on their own event name because they are
  // a different kind of thing and a reader must be able to tell — an activity
  // record is on the chain and will be there tomorrow, a session frame is a
  // window on a run and is gone when it rolls.
  //
  // `item` scopes the whole subscription to one unit, server-side. A card
  // following one run has no use for the factory's feed, and on a busy daemon
  // the difference is between a stream a page renders and one it discards.
  constructor(host, { limit = 100, onEvent, onSession, item } = {}) {
    this._host = host;
    this._limit = limit;
    this._onEvent = onEvent;
    this._onSession = onSession;
    this._item = item;
    host.addController(this);
  }

  hostConnected() {
    if (!this._item) {
      getActivity().then((initial) => {
        this.events = (initial ?? []).slice(-this._limit);
        this._host.requestUpdate();
      }).catch(() => { /* the stream below still tells the story */ });
    }
    this._es = new EventSource(this._item ? `/v1/events?item=${encodeURIComponent(this._item)}` : '/v1/events');
    this._es.addEventListener('hello', () => {
      this.connected = true;
      this._host.requestUpdate();
    });
    this._es.addEventListener('session', (e) => {
      this._onSession?.(JSON.parse(e.data));
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
