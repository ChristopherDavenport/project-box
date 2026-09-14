import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tally, cardTitle, sessionHeading, currentSession, cardStage, roleSplit, roleSplitLine, factCite, originTotals } from './format.js';

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

const SHA1 = 'aaaaaaaaaaaaaaaaaaaa';
const SHA2 = 'bbbbbbbbbbbbbbbbbbbb';

test('sessionHeading folds a builder→gate(pass)→builder→gate(fail) chain into role-headed rows', () => {
  const chain = [
    { role: 'builder', attempt: 1, of: 2, finished: true, sha: SHA1 },
    { role: 'gate', sha: SHA1, decision: 'pass', reason: 'Looks good overall.\nNothing else to add.', verdict_record_id: 'rec1' },
    { role: 'builder', attempt: 2, of: 2, finished: true, sha: SHA2 },
    { role: 'gate', sha: SHA2, decision: 'fail', reason: 'Broke two tests in package foo.\nStack trace follows.', verdict_record_id: 'rec2' },
  ];
  const [b1, g1, b2, g2] = chain.map(sessionHeading);
  assert.equal(b1.role, 'builder');
  assert.equal(b1.roleAppearance, 'brand');
  assert.equal(b1.text, 'attempt 1 of 2 · built aaaaaaaaaa');
  assert.equal(g1.role, 'gate');
  assert.equal(g1.roleAppearance, 'neutral');
  assert.deepEqual(g1.verdict, { decision: 'pass', appearance: 'positive', reason: 'Looks good overall.', recordId: 'rec1' });
  assert.equal(b2.text, 'attempt 2 of 2 · built bbbbbbbbbb');
  assert.deepEqual(g2.verdict, { decision: 'fail', appearance: 'negative', reason: 'Broke two tests in package foo.', recordId: 'rec2' });
});

test('sessionHeading reads a gate row with no verdict yet as "reviewing <sha>"', () => {
  const h = sessionHeading({ role: 'gate', sha: SHA1 });
  assert.equal(h.text, 'reviewing aaaaaaaaaa');
  assert.equal(h.verdict, undefined);
});

test('sessionHeading reads a planner row as its own tag with the attempt alone', () => {
  const h = sessionHeading({ role: 'planner', attempt: 2 });
  assert.equal(h.role, 'planner');
  assert.equal(h.roleAppearance, 'positive');
  assert.equal(h.text, 'attempt 2');
});

test('currentSession picks the newest unfinished row, else the newest', () => {
  const rows = [
    { role: 'builder', finished: true },
    { role: 'gate', finished: true },
    { role: 'builder', finished: false },
  ];
  assert.equal(currentSession(rows), rows[2]);
  assert.equal(currentSession(rows.slice(0, 2)), rows[1]);
  assert.equal(currentSession([]), null);
  assert.equal(currentSession(undefined), null);

  // A later finished attempt does not displace an earlier still-open one.
  const stale = [
    { role: 'builder', finished: true },
    { role: 'gate', finished: false },
    { role: 'builder', finished: true },
  ];
  assert.equal(currentSession(stale), stale[1]);
});

test('cardStage names the model behind a bare "building" backend stage when the current session is actually reviewing', () => {
  const s = { stage: 'building', agent_sessions: [{ role: 'gate', model: 'claude-sonnet-5', finished: false, sha: SHA1 }] };
  assert.deepEqual(cardStage(s), { label: 'reviewing', model: 'claude-sonnet-5' });
});

test('cardStage leaves a non-building backend stage untouched, naming no model', () => {
  const s = { stage: 'gating', agent_sessions: [{ role: 'gate', model: 'claude-sonnet-5', finished: true, decision: 'pass' }] };
  assert.deepEqual(cardStage(s), { label: 'gating', model: '' });
});

test("cardStage names a judgement card's planning stage, which the backend never sets", () => {
  const s = { judgement: true, agent_sessions: [{ role: 'planner', model: 'claude-opus-5', finished: false }] };
  assert.deepEqual(cardStage(s), { label: 'planning', model: 'claude-opus-5' });
});

test('cardStage returns null when there is no stage to show', () => {
  assert.equal(cardStage({}), null);
});

test('roleSplit counts each unit once by its current session\'s role, and roleSplitLine renders it', () => {
  const sessions = [
    { agent_sessions: [{ role: 'builder', finished: false }] },
    { agent_sessions: [{ role: 'builder', attempt: 1, finished: true }, { role: 'builder', attempt: 2, finished: false }] },
    { agent_sessions: [{ role: 'builder', finished: true }, { role: 'gate', finished: false }] },
    { judgement: true, agent_sessions: [{ role: 'planner', finished: false }] },
  ];
  const counts = roleSplit(sessions);
  assert.deepEqual(counts, { building: 2, reviewing: 1, planning: 1 });
  assert.equal(roleSplitLine(counts), 'building 2 · reviewing 1 · planning 1');
});

test('roleSplitLine omits a role holding nothing', () => {
  assert.equal(roleSplitLine({ building: 1, reviewing: 0, planning: 0 }), 'building 1');
});

test('factCite renders tenant/scope#seq exactly as the API states them', () => {
  assert.equal(factCite({ tenant: 't1', scope: 'workitem/wi-abc', seq: 42 }), 't1/workitem/wi-abc#42');
});

test('factCite tolerates an absent FactRef rather than throwing', () => {
  assert.equal(factCite(undefined), '/#0');
});

test('originTotals renders known figures when every session is covered', () => {
  const o = originTotals({ sessions: 2, covered: 2, attempts: 3, tokens: 1_200_000, cost_usd: 4.5, wall_ms: 90_000 });
  assert.equal(o.tokens, '1.2M');
  assert.equal(o.cost, '$4.50');
  assert.equal(o.wallSec, 90);
  assert.equal(o.partial, false);
});

test('originTotals renders a partial sum, flagged, when some sessions are uncovered', () => {
  const o = originTotals({ sessions: 3, covered: 1, tokens: 500, cost_usd: 0.01, wall_ms: 1000 });
  assert.equal(o.tokens, '500');
  assert.equal(o.partial, true);
  assert.equal(o.covered, 1);
  assert.equal(o.sessions, 3);
});

test('originTotals never reads unknown spend as zero when nothing is covered', () => {
  const o = originTotals({ sessions: 2, covered: 0, attempts: 1 });
  assert.equal(o.tokens, 'unknown');
  assert.equal(o.cost, 'unknown');
  assert.equal(o.wallSec, null);
});
