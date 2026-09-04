import { getActivity } from './api.js';

// One EventSource for the factory feed, shared by every subscriber on the
// page. The header's live indicator, the claims strip and the view all want
// the same stream; opening one connection each spent the browser's per-origin
// budget on copies of a feed that is identical by definition.
//
// Item-scoped subscriptions stay their own connection: the server filters
// them, and on a busy daemon the difference is between a stream a card
// renders and one it discards.
const feed = { es: null, subs: new Set(), connected: false };

function open() {
  feed.es = new EventSource('/v1/events');
  feed.es.addEventListener('hello', () => {
    feed.connected = true;
    for (const s of feed.subs) s.onState?.(true);
  });
  feed.es.addEventListener('session', (e) => {
    const f = JSON.parse(e.data);
    for (const s of feed.subs) s.onSession?.(f);
  });
  feed.es.addEventListener('activity', (e) => {
    const a = JSON.parse(e.data);
    for (const s of feed.subs) s.onEvent?.(a);
  });
  feed.es.onerror = () => {
    feed.connected = false;
    for (const s of feed.subs) s.onState?.(false);
  };
}

function subscribe(sub) {
  feed.subs.add(sub);
  if (!feed.es) open();
  else sub.onState?.(feed.connected);
  return () => {
    feed.subs.delete(sub);
    if (feed.subs.size === 0) {
      feed.es?.close();
      feed.es = null;
      feed.connected = false;
    }
  };
}

// ActivityController: a reactive controller owning a subscription —
// lifecycle-aware setup and teardown, per the Lit controller pattern for
// ongoing resources.
export class ActivityController {
  events = [];
  connected = false;

  // onEvent fires for every activity record as it lands, so a view that
  // renders derived state — the queue, one item's timeline — can refresh
  // itself instead of making the operator reload the page.
  //
  // onSession fires for the other half: the frames a running agent is
  // producing right now. They arrive on their own event name because they are
  // a different kind of thing and a reader must be able to tell — an activity
  // record is on the chain and will be there tomorrow, a session frame is a
  // window on a run and is gone when it rolls.
  //
  // `item` scopes the whole subscription to one unit, server-side.
  constructor(host, { limit = 100, onEvent, onSession, item } = {}) {
    this._host = host;
    this._limit = limit;
    this._onEvent = onEvent;
    this._onSession = onSession;
    this._item = item;
    host.addController(this);
  }

  hostConnected() {
    const sub = {
      onState: (up) => { this.connected = up; this._host.requestUpdate(); },
      onSession: (f) => this._onSession?.(f),
      onEvent: (a) => {
        // New reference, never a mutation: Lit change detection is `!==`.
        this.events = [...this.events, a].slice(-this._limit);
        this._host.requestUpdate();
        this._onEvent?.(a);
      },
    };
    if (this._item) {
      this._es = new EventSource(`/v1/events?item=${encodeURIComponent(this._item)}`);
      this._es.addEventListener('hello', () => sub.onState(true));
      this._es.addEventListener('session', (e) => sub.onSession(JSON.parse(e.data)));
      this._es.addEventListener('activity', (e) => sub.onEvent(JSON.parse(e.data)));
      this._es.onerror = () => sub.onState(false);
      return;
    }
    if (this._limit > 1) {
      getActivity().then((initial) => {
        this.events = (initial ?? []).slice(-this._limit);
        this._host.requestUpdate();
      }).catch(() => { /* the stream below still tells the story */ });
    }
    this._unsub = subscribe(sub);
  }

  hostDisconnected() {
    this._es?.close();
    this._es = undefined;
    this._unsub?.();
    this._unsub = undefined;
  }
}
