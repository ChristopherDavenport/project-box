import { test } from 'node:test';
import assert from 'node:assert/strict';
import { settled, orderList, planeOrder } from './order.js';

test('settled predicate over states, issue #205 case c', () => {
  assert.equal(settled('claimed', [{ finished: true }]), false);
  assert.equal(settled('waiting', [{ finished: true }]), false);
  assert.equal(settled('done', [{ finished: true }, { finished: true }]), true);
  assert.equal(settled('done', [{ finished: true }, { finished: false }]), false);
  assert.equal(settled('parked', [{ finished: true }]), true);
  assert.equal(settled('failed', [{ finished: true }]), true);
  assert.equal(settled('resolved', [{ finished: true }]), true);
  // no sessions at all is vacuously all-finished — a terminal state with
  // nothing recorded yet reads as settled, not stuck live forever.
  assert.equal(settled('done', []), true);
  assert.equal(settled('done', undefined), true);
});

test('a live unit orders sessions newest first and frames newest first', () => {
  const s1 = { agentref: 's1', finished: true };
  const s2 = { agentref: 's2', finished: false };
  const roots = [s1, s2]; // chain order: opened first, first
  const childrenOf = new Map();
  const framesOf = (ref) => (ref === 's1' ? ['s1-f1', 's1-f2'] : ['s2-f1', 's2-f2']);

  assert.equal(settled('claimed', roots), false);
  const order = planeOrder(true, roots, childrenOf, framesOf);
  assert.deepEqual(order.roots, [s2, s1]);
  assert.deepEqual(order.framesOf('s1'), ['s1-f2', 's1-f1']);
  assert.deepEqual(order.framesOf('s2'), ['s2-f2', 's2-f1']);
});

test('a settled unit orders sessions oldest first and frames oldest first', () => {
  const s1 = { agentref: 's1', finished: true };
  const s2 = { agentref: 's2', finished: true };
  const roots = [s1, s2];
  const childrenOf = new Map();
  const framesOf = (ref) => (ref === 's1' ? ['s1-f1', 's1-f2'] : ['s2-f1', 's2-f2']);

  assert.equal(settled('done', roots), true);
  const order = planeOrder(false, roots, childrenOf, framesOf);
  assert.deepEqual(order.roots, [s1, s2]);
  assert.deepEqual(order.framesOf('s1'), ['s1-f1', 's1-f2']);
  assert.deepEqual(order.framesOf('s2'), ['s2-f1', 's2-f2']);
});

test('a child list gets the same flip as its parent', () => {
  const root = { agentref: 'r' };
  const c1 = { agentref: 'c1' };
  const c2 = { agentref: 'c2' };
  const childrenOf = new Map([['r', [c1, c2]]]);
  const framesOf = () => [];

  const live = planeOrder(true, [root], childrenOf, framesOf);
  assert.deepEqual(live.childrenOf.get('r'), [c2, c1]);

  const record = planeOrder(false, [root], childrenOf, framesOf);
  assert.deepEqual(record.childrenOf.get('r'), [c1, c2]);
});

test('evidence ordering flips with the same predicate, issue #205 case d', () => {
  const evidence = [{ At: 1 }, { At: 2 }, { At: 3 }]; // chain order, oldest first, as the server sends it

  const runningItem = settled('claimed', []);
  assert.deepEqual(orderList(!runningItem, evidence), [{ At: 3 }, { At: 2 }, { At: 1 }]);

  const doneItem = settled('done', [{ finished: true }]);
  assert.deepEqual(orderList(!doneItem, evidence), [{ At: 1 }, { At: 2 }, { At: 3 }]);
});
