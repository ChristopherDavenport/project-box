import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tally, cardTitle } from './format.js';

const FULL = {
  backlog: { by_state: { done: 42, pending: 3 }, pending: 3, total: 45 },
  pending: 3,
  spend: {
    tokens: 1_200_000,
    cost_micros: 4_500_000,
    unpriced: 0,
    unmetered: 0,
    running: 2,
    boot_tokens: 9_000_000,
    boot_cost_micros: 31_000_000,
    boot_unpriced: 0,
    boot_unmetered: 0,
  },
};

test('tally reads the full fixture as straight projections', () => {
  const t = tally(FULL);
  assert.equal(t.landed, 42);
  assert.equal(t.pending, 3);
  assert.equal(t.running, 2);
  assert.equal(t.windowMoney, '$4.50');
  assert.equal(t.windowTokens, '1.2M');
  assert.equal(t.bootMoney, '$31.00');
  assert.equal(t.bootTokens, '9M');
});

test('unpriced > 0 renders the window figures unknown, never a partial number', () => {
  const t = tally({ ...FULL, spend: { ...FULL.spend, unpriced: 1 } });
  assert.equal(t.windowMoney, 'unknown');
  assert.equal(t.windowTokens, 'unknown');
  // boot figures are unaffected by a window-scoped unpriced charge.
  assert.equal(t.bootMoney, '$31.00');
});

test('unmetered > 0 renders the window figures unknown, never a partial number', () => {
  const t = tally({ ...FULL, spend: { ...FULL.spend, unmetered: 1 } });
  assert.equal(t.windowMoney, 'unknown');
  assert.equal(t.windowTokens, 'unknown');
});

test('boot unpriced/unmetered renders only the boot figures unknown', () => {
  const t = tally({ ...FULL, spend: { ...FULL.spend, boot_unpriced: 1 } });
  assert.equal(t.windowMoney, '$4.50');
  assert.equal(t.bootMoney, 'unknown');
  assert.equal(t.bootTokens, 'unknown');
});

test('a daemon predating boot figures leaves them null, not zero', () => {
  const { boot_tokens, boot_cost_micros, boot_unpriced, boot_unmetered, ...older } = FULL.spend;
  const t = tally({ ...FULL, spend: older });
  assert.equal(t.bootMoney, null);
  assert.equal(t.bootTokens, null);
  // the window figures are unaffected by the daemon's age.
  assert.equal(t.windowMoney, '$4.50');
});

test('spend absent entirely renders no zeros', () => {
  const t = tally({ backlog: { by_state: { done: 5 } }, pending: 1 });
  assert.equal(t.landed, 5);
  assert.equal(t.pending, 1);
  assert.equal(t.running, null);
  assert.equal(t.windowMoney, null);
  assert.equal(t.windowTokens, null);
  assert.equal(t.bootMoney, null);
  assert.equal(t.bootTokens, null);
});

test('an empty status renders landed and pending as zero, not unknown', () => {
  const t = tally({});
  assert.equal(t.landed, 0);
  assert.equal(t.pending, 0);
});

test('cardTitle clamps a 200-char unbroken summary and keeps the full text for a tooltip', () => {
  const summary = 'x'.repeat(200);
  const { label, full } = cardTitle(summary);
  assert.equal(full, summary);
  assert.ok(label.length < summary.length, 'label must be shorter than the source string');
  assert.ok(label.endsWith('…'), 'a clamped label ends with the ellipsis clip() uses');
});

test('cardTitle leaves a title that already fits untouched', () => {
  const { label, full } = cardTitle('Fix the parser');
  assert.equal(label, 'Fix the parser');
  assert.equal(full, 'Fix the parser');
});

test('cardTitle reads a missing title as empty, never the string "undefined"', () => {
  const { label, full } = cardTitle(undefined);
  assert.equal(label, '');
  assert.equal(full, '');
});
