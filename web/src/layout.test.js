// A regression tripwire, not a layout assertion: this repo carries no
// browser test runner (web/embed.go's `go generate` runs `npm ci`, so adding
// one would pull a browser onto every fresh-checkout gate build across the
// whole factory), so the scrollWidth-at-three-widths check issue #221 asks
// for is out of reach here. What a plain node test over the source text CAN
// hold the line on is that the specific rules the overflow fix depends on
// don't quietly disappear in a later edit.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const pages = readFileSync(fileURLToPath(new URL('./pages.js', import.meta.url)), 'utf8');
const app = readFileSync(fileURLToPath(new URL('./app.js', import.meta.url)), 'utf8');

test('the sessions grid track floors at min(380px, 100%), not a bare 380px', () => {
  assert.match(pages, /grid-template-columns:\s*repeat\(auto-fit,\s*minmax\(min\(380px,\s*100%\),\s*1fr\)\)/);
});

test('the sessions grid children carry min-width: 0 so the track can actually shrink', () => {
  assert.match(pages, /\.grid > \* \{ min-width: 0; \}/);
});

test('every .meta and .session-meta row clamps its children against one-long-token overflow', () => {
  const metaRules = pages.match(/\.meta \{ display: flex;/g) ?? [];
  const metaGuards = pages.match(/\.meta > \* \{ min-width: 0; overflow-wrap: anywhere; \}/g) ?? [];
  const sessionMetaRules = pages.match(/\.session-meta \{ display: flex;/g) ?? [];
  const sessionMetaGuards = pages.match(/\.session-meta > \* \{ min-width: 0; overflow-wrap: anywhere; \}/g) ?? [];
  assert.ok(metaRules.length > 0, 'expected at least one .meta rule');
  assert.ok(sessionMetaRules.length > 0, 'expected at least one .session-meta rule');
  // One guard per row definition — a class this file defines several times
  // over (one per view), each place it is redefined must carry its own
  // overflow guard rather than relying on one view's fix to cover another's.
  assert.equal(metaGuards.length, metaRules.length, 'every .meta rule needs its own min-width:0/overflow-wrap:anywhere guard');
  assert.equal(sessionMetaGuards.length, sessionMetaRules.length, 'every .session-meta rule needs its own min-width:0/overflow-wrap:anywhere guard');
});

test('the app shell backstops with overflow-x: hidden on its outermost host', () => {
  assert.match(app, /:host \{[^}]*overflow-x: hidden;[^}]*\}/);
});
