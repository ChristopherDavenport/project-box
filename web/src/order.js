// The room orders the agent plane one way while a unit is live and another
// once it is settled, per issue #205: live reads as a feed (newest session
// first, newest frame first, top of page) because the question is what is
// happening now; settled reads as a record (first session first, frames from
// the first prompt to the last answer) because the question is what
// happened. Pulled out of pages.js so the flip itself is pure and testable
// without a DOM — see order.test.js — and so every surface that renders the
// plane (box-sessions, box-item, box-ingress) applies the exact same rule
// rather than each guessing its own.

const SETTLED_STATES = new Set(['done', 'parked', 'failed', 'resolved']);

/** settled says whether a unit's plane has stopped changing: its own state is
 *  one of the terminal ones AND every session row the chain has recorded for
 *  it has finished. Both halves matter — the state can go terminal a beat
 *  before the last session's own frames are in, and the render must not flip
 *  to record order until there is nothing left to feed. */
export function settled(state, sessions) {
  return SETTLED_STATES.has(state) && (sessions ?? []).every((s) => s.finished);
}

/** orderList applies the one flip: newest-first while live, left as given
 *  (chain order, oldest first) once settled. Every ordered surface in the
 *  plane — a session list, a session's children, one session's frames, a
 *  unit's evidence chain — uses this same function, never its own reverse. */
export function orderList(live, list) {
  return live ? (list ?? []).slice().reverse() : (list ?? []).slice();
}

/** planeOrder applies orderList across a whole session forest — the roots,
 *  every session's own children, and each session's frames (read through
 *  `framesOf`, e.g. sessionForest's `byRef.get`) — so a render walks one
 *  ordered structure instead of re-deriving the flip at every level. Input
 *  is always chain order (oldest first, as sessionForest/continuationTree
 *  hand it over); this never resorts by timestamp, it only decides whether
 *  to read that chain forwards or backwards. */
export function planeOrder(live, roots, childrenOf, framesOf) {
  const orderedChildrenOf = new Map();
  for (const [ref, kids] of childrenOf ?? []) orderedChildrenOf.set(ref, orderList(live, kids));
  return {
    roots: orderList(live, roots),
    childrenOf: orderedChildrenOf,
    framesOf: (ref) => orderList(live, framesOf(ref)),
  };
}
