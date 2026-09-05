// Formatting: every raw value the API returns, rendered the way an operator
// reads it. Hashes are shortened, durations rounded to the precision that
// matters, timestamps say how long ago, and evidence records are described in
// words. Nothing here fetches; nothing in the views spells a record kind.

/** age renders a duration at the precision an operator cares about. */
export function age(sec) {
  if (sec == null) return '';
  sec = Math.max(0, Math.round(sec));
  if (sec < 90) return `${sec}s`;
  if (sec < 5400) return `${Math.floor(sec / 60)}m`;
  if (sec < 172800) return `${Math.floor(sec / 3600)}h`;
  return `${Math.floor(sec / 86400)}d`;
}

export function firstLine(s) {
  s = String(s ?? '');
  const i = s.indexOf('\n');
  return (i < 0 ? s : s.slice(0, i)).trim();
}

export function clip(s, n = 200) {
  s = String(s ?? '');
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

/** title is the one line a unit is known by: its summary, when the record
 *  carries one, else the first line of its task — the reading every record
 *  written before `summary` existed still gets. A bare task string (no
 *  backing record, as on the open-ingress form) is accepted directly. */
export function title(item) {
  return item?.summary || firstLine(typeof item === 'string' ? item : item?.task);
}

/** taskURL is the first link in a task's text — an issue, usually. */
export function taskURL(task) {
  const m = String(task ?? '').match(/https?:\/\/[^\s)>\]]+/);
  return m ? m[0] : '';
}

/** short abbreviates a hex object id the way git does; anything else passes. */
export function short(h, n = 10) {
  const s = String(h ?? '');
  return /^[0-9a-f]{16,}$/i.test(s) ? s.slice(0, n) : s;
}

const ABS = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' });

/** when renders a timestamp as how long ago, with the absolute form beside it. */
export function when(iso) {
  if (!iso) return { rel: '', abs: '', iso: '' };
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return { rel: String(iso), abs: '', iso: '' };
  const sec = Math.round((Date.now() - d.getTime()) / 1000);
  // A minute of skew between the daemon's clock and the browser's reads as
  // "just now", not as a record from the future.
  const rel = sec < -60 ? `in ${age(-sec)}` : sec < 5 ? 'just now' : `${age(sec)} ago`;
  return { rel, abs: ABS.format(d), iso: d.toISOString() };
}

/** Tokens at a glance: an operator reads 1.2M, not 1203481. */
export function fmtTokens(n) {
  if (n == null) return '0';
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${Math.round(n / 100) / 10}k`;
  return `${Math.round(n / 100_000) / 10}M`;
}

/**
 * fmtUSD renders micro-dollars as money. Sub-cent figures keep four decimals
 * because a single run's cost is often smaller than a cent and rounding it to
 * $0.00 is the same lie as counting an unreported cost as zero.
 */
export function fmtUSD(micros) {
  const usd = (micros ?? 0) / 1e6;
  return usd < 1 ? `$${usd.toFixed(4)}` : `$${usd.toFixed(2)}`;
}

/**
 * tokenSplit is the total broken into what it is billed as. A cached read
 * costs roughly a tenth of a fresh input token and an output token several
 * times either, so two windows with the same total can differ on the invoice
 * by an order of magnitude. Kinds that did not occur are left out.
 */
export function tokenSplit(s) {
  const parts = [
    ['in', s?.input],
    ['out', s?.output],
    ['cache read', s?.cache_read],
    ['cache write', s?.cache_write],
  ].filter(([, n]) => n > 0);
  return parts.map(([k, n]) => `${k} ${fmtTokens(n)}`).join(' · ');
}

/** A tool tally as one line, busiest first — the runaway signature is a name
 *  with a big number next to it. */
export function tools(t) {
  return Object.entries(t ?? {})
    .sort((a, b) => b[1] - a[1])
    .map(([name, n]) => (n > 1 ? `${name}×${n}` : name))
    .join(' ');
}

/** origin is where a unit came from, in the operator's words: the route and
 *  the occurrence key. The ingress id is a record locator, not an origin, and
 *  is never shown here. */
export function origin(e) {
  if (e?.route) return e.key ? `${e.route} / ${e.key}` : e.route;
  return '';
}

/** place is which box and repo a unit belongs to. */
export function place(e) {
  return [e?.box, e?.repo].filter(Boolean).join(' / ');
}

/** hold renders who has an item and how much of their claim is left. */
export function hold(e) {
  if (!e?.runner) return '';
  const left = e.lease_expires_sec ?? 0;
  return e.lease_lapsed
    ? `${e.runner} — lease lapsed ${age(-left)} ago`
    : `${e.runner} — ${age(left)} left`;
}

/** why renders the graph reason a row is not moving: its unsettled
 *  dependencies, or the item a cascade actually started at. */
export function why(e) {
  if (e?.blocked_by?.length) return e.blocked_by.join(', ');
  if (e?.root) return `cascaded from ${e.root}`;
  return '';
}

/** breakdown renders which states the queue is made of, alphabetically — the
 *  states have no rank, and an object's key order would make two identical
 *  statuses read differently. */
export function breakdown(by) {
  return Object.keys(by ?? {})
    .sort()
    .map((k) => `${k} ${by[k]}`)
    .join(' · ');
}

// Colour is a second channel beside the word, never the only one. The jh-tag
// hooks these map onto are set per class in the views' styles.
export const STATE_APPEARANCE = {
  pending: 'neutral', claimed: 'brand', waiting: 'neutral', handedoff: 'neutral',
  parked: 'negative', failed: 'negative', done: 'positive', resolved: 'positive',
};

export const KIND_APPEARANCE = {
  done: 'positive', land: 'positive', check: 'positive', resume: 'positive', 'config-reloaded': 'positive',
  recovered: 'positive', resolved: 'positive', started: 'positive', promoted: 'positive', 'build-resumed': 'positive',
  parked: 'negative', failed: 'negative', 'budget-refused': 'negative', stop: 'negative', denied: 'negative',
  escalated: 'negative', 'land-refused': 'negative', 'land-conflict': 'negative', 'rebase-refused': 'negative',
  'handoff-failed': 'negative', 'break-glass': 'negative', preempted: 'negative',
  claimed: 'brand', 'agent-run': 'brand', 'agent-session': 'brand', retried: 'brand',
  planned: 'positive', 'plan-failed': 'negative', 'plan-retried': 'brand', 'plan-resolved': 'positive',
};

export const SEVERITY_APPEARANCE = { low: 'neutral', medium: 'neutral', high: 'negative', critical: 'negative' };

export const appearanceOf = (kind) => KIND_APPEARANCE[kind] ?? 'neutral';

const utf8 = new TextDecoder();

/**
 * bodyOf decodes a record's body whichever way it arrived. The chain
 * endpoints marshal Go's []byte as base64 under `Body`; the activity feed
 * pre-decodes into `body`, JSON when it parses and a string otherwise. Either
 * way the result is an object for structured records and a string for prose.
 *
 * Decoded as UTF-8 on purpose: atob alone hands back Latin-1, which turned
 * every em dash in a task into three bytes of mojibake.
 */
export function bodyOf(r) {
  if (r.body !== undefined && r.body !== null) return r.body;
  if (r.Body == null || r.Body === '') return '';
  let s;
  try {
    s = utf8.decode(Uint8Array.from(atob(r.Body), (c) => c.charCodeAt(0)));
  } catch {
    return '';
  }
  try { return JSON.parse(s); } catch { return s; }
}

/** norm folds the two record spellings — Go field names from the chain,
 *  json tags from the feed — into one shape the views read. */
export function norm(r) {
  return {
    kind: r.kind ?? r.Kind ?? '',
    subject: r.subject ?? r.Subject ?? '',
    scope: r.scope ?? r.Scope ?? '',
    actor: r.actor ?? r.Actor ?? '',
    at: r.at ?? r.At ?? '',
    seq: r.seq ?? r.Seq ?? 0,
    body: bodyOf(r),
  };
}

// Fields of a state record that are the item, not the event.
const ITEM_FIELDS = new Set(['id', 'tenant', 'box', 'repo', 'ingress', 'requester', 'task', 'state', 'opened', 'changed', 'depth']);

/** summarize renders the scalar fields of an unknown structured body as
 *  "key: value" pairs — never the whole blob, and never the task text. */
export function summarize(o, max = 4) {
  return Object.entries(o ?? {})
    .filter(([k, v]) => !ITEM_FIELDS.has(k) && v !== '' && v != null && typeof v !== 'object')
    .slice(0, max)
    .map(([k, v]) => `${k}: ${short(String(v))}`)
    .join(' · ');
}

function noteOf(o) {
  return o?.note ? ` — ${firstLine(o.note)}` : '';
}

function reasonOf(o, text) {
  const r = o?.reason ?? o?.Reason ?? text;
  return r ? firstLine(r) : '';
}

/**
 * describe renders a record as the one line a watcher wants.
 *
 * The item's own state records carry the whole WorkItem as their body; what a
 * reader wants from them is the transition and the note that explains it —
 * never the task text, which is the page's title already, and never raw
 * JSON, which is what the timeline showed for two days before anyone opened
 * an item and found a 13,000-pixel-wide page.
 *
 * `titles` maps item ids to their tasks, so index records about an item can
 * say what the item is rather than which hash it has.
 */
export function describe(raw, titles = {}) {
  const r = norm(raw);
  const o = r.body && typeof r.body === 'object' ? r.body : null;
  const text = typeof r.body === 'string' ? r.body : '';
  const name = (id) => (titles[id] ? title(titles[id]) : id);
  switch (r.kind) {
    case 'queued':
      return 'Queued' + (o?.depends_on?.length ? `, after ${o.depends_on.join(', ')}` : '') + noteOf(o);
    case 'claimed':
      return `Claimed${o?.seat ? ` for seat ${o.seat}` : ''}${noteOf(o)}`;
    case 'waiting':
      return `Waiting${noteOf(o)}`;
    case 'handedoff':
      return `Handed off to a fresh session${noteOf(o)}`;
    case 'parked':
      return `Parked${noteOf(o)}`;
    case 'failed':
      return `Failed${noteOf(o)}`;
    case 'recovered':
      return `Recovered from a lapsed claim${noteOf(o)}`;
    case 'done':
      return `Done — landed as ${short(r.subject)}`;
    case 'built':
      return `Built ${short(o?.built || r.subject)}`;
    case 'land':
      return o?.branch
        ? `Landed on ${[o.repo, o.branch].filter(Boolean).join('/')}: ${short(o.was)} → ${short(o.now ?? r.subject)}`
        : `Landed ${short(r.subject)}`;
    case 'gate':
      return `${o?.Gate ?? o?.gate ?? 'gate'}: ${o?.Decision ?? o?.decision ?? ''}${reasonOf(o) ? ' — ' + reasonOf(o) : ''}`;
    case 'check':
      if (o && 'green' in o) return `Check ${o.green ? 'green' : 'RED'}${o.lane ? ` on the ${o.lane} lane` : ''}`;
      return o ? `Check ${o.Decision ?? o.decision ?? ''} ${reasonOf(o)}`.trim() : `Check ${firstLine(text)}`.trim();
    case 'agent-run':
      return `${o?.seat ?? 'agent'} started as ${o?.role ?? 'builder'} (mode ${o?.mode ?? '?'})`;
    case 'agent-prompt':
      return 'Prompt materialized for the agent';
    case 'agent-result': {
      if (!o) return firstLine(text);
      const bits = [`${o.stop}`, `${Math.round((o.wall_ms ?? 0) / 100) / 10}s`];
      // Unmetered is the fact worth seeing: an unreported cost is not a
      // zero cost, and it halts the window.
      bits.push(o.metered ? `${fmtTokens(o.tokens)} tokens` : 'cost NOT reported');
      if (o.cost_usd) bits.push(`$${o.cost_usd.toFixed(4)}`);
      // Which tools, not how many — a count cannot tell an ordinary run
      // from a runaway, because both are a number.
      bits.push(o.tools ? tools(o.tools) : `${o.tool_calls ?? 0} tools`);
      if (o.denied_tools) bits.push(`DENIED ${tools(o.denied_tools)}`);
      if (o.unknown_updates?.length) bits.push(`dialect drift: ${o.unknown_updates.join(', ')}`);
      return bits.join(' · ');
    }
    case 'planned': {
      // The decomposition: how many units, judged or derived, and where
      // each is expressed. The plan is the artifact between ingress and
      // work item, and it is the one that used to leave no record at all.
      if (!o) return firstLine(text);
      const n = (o.nodes ?? []).length;
      const who = o.replayable ? o.decomposer : `${o.decomposer} (judgement)`;
      const where = (o.nodes ?? []).map((u) => (u.sections?.length ? `${u.id} in ${u.sections.join('+')}` : u.id)).join(', ');
      return `Planned ${n} unit${n === 1 ? '' : 's'} by ${who}${where ? ': ' + where : ''}`;
    }
    case 'plan-failed':
      return `Planning failed — ${firstLine(text)}`;
    case 'plan-retried':
      return `Planning retried${text ? ' — ' + firstLine(text) : ''}`;
    case 'plan-resolved':
      return `Planning resolved${text ? ' — ' + firstLine(text) : ''}`;
    case 'agent-session': {
      // What the agent said it was once the session opened — the model that
      // actually ran, the mode in force, every other option, and how many
      // commands (skills among them) it offered. The agent-run record says
      // what was asked, as a digest; this says what was answered.
      if (!o) return firstLine(text);
      const bits = [`model ${o.model || 'not exposed'}`];
      if (o.modes?.current) bits.push(`mode ${o.modes.current}`);
      for (const c of o.config ?? []) if (c.id !== 'model' && c.id !== 'mode') bits.push(`${c.id} ${c.current}`);
      bits.push(`${(o.commands ?? []).length} commands offered`);
      return bits.join(' · ');
    }
    case 'charge': {
      // The charge body IS the spend, split and priced. An unreported charge
      // says so; a zero here would be the same lie the meter refuses to tell.
      if (!o) return firstLine(text);
      if (!o.metered) return 'Cost NOT reported — claims stopped';
      const bits = [`${fmtTokens(o.tokens ?? 0)} tokens`];
      const split = tokenSplit(o);
      if (split) bits.push(split);
      if (o.cost_micros) bits.push(fmtUSD(o.cost_micros));
      return bits.join(' · ');
    }
    case 'spend': {
      if (o?.tokens != null) return `Spent ${fmtTokens(o.tokens)} tokens`;
      if (o?.running != null) return `Run started — ${o.running} running, ${o.started_this_window ?? '?'} started this window`;
      return `Spend recorded${text ? ' — ' + firstLine(text) : ''}`;
    }
    case 'budget-refused': {
      if (o?.max_per_window != null) return `Refused by the budget — ${o.max_per_window} starts per ${o.window ?? 'window'} already used`;
      if (o?.max_tokens != null) return `Refused by the budget — ${fmtTokens(o.max_tokens)} tokens per window already spent`;
      return `Refused by the budget${reasonOf(o, text) ? ' — ' + reasonOf(o, text) : summarize(o) ? ' — ' + summarize(o) : ''}`;
    }
    case 'authz':
      return reasonOf(o, text) || 'Authorization checked';
    case 'escalated':
      return `Escalated: ${reasonOf(o, text)}`;
    case 'handoff-requested':
      return `Handoff requested: ${reasonOf(o, text)}`;
    case 'handoff-failed':
      return `Handoff failed: ${reasonOf(o, text)}`;
    case 'denied':
      return `Tool refused by policy: ${o?.tool ?? reasonOf(o, text)}`;
    case 'retried':
      return `Retried by an operator${text ? ' — ' + firstLine(text) : ''}`;
    case 'resolved':
      return `Resolved by an operator${text ? ' — ' + firstLine(text) : ''}`;
    case 'stop':
      return `Claims stopped — ${firstLine(text)}`;
    case 'resume':
      return `Claims resumed — ${firstLine(text)}`;
    case 'config-reloaded':
      return `Configuration reloaded${text ? ': ' + firstLine(text) : o ? ': ' + summarize(o) : ''}`;
    case 'ingress-open':
      return `Ingress opened${o?.route ? ` on ${o.route}` : ''}${o?.key ? ` for ${o.key}` : ''}`;
    case 'indexed':
      // On the chain the note is the body; on the live feed the body may be
      // the indexed thing itself, with the note as a field of it.
      return indexLine(r, text || o?.note || '', name);
    case 'compacted':
      return 'Chain compacted behind a verifiable seal';
    case 'review-sample':
      return 'Sampled for human review';
    case 'review-signoff':
      return `Signed off${text ? ' — ' + firstLine(text) : ''}`;
    case 'break-glass':
      return `Break-glass: ${reasonOf(o, text)}`;
    case 'preempted':
      return `Preempted by an incident${reasonOf(o, text) ? ' — ' + reasonOf(o, text) : ''}`;
    default:
      if (text) return clip(firstLine(text));
      if (o) return clip(summarize(o)) || r.kind;
      return r.subject;
  }
}

// An index record is the factory filing an item under a listing. Say which
// listing in the operator's words, and name the item rather than its hash.
function indexLine(r, note, name) {
  const list = r.scope.replace(/^index\//, '');
  const who = name(r.subject);
  // The pending index's note is the task itself, which is also the name:
  // saying it twice reads as a stutter, not as more information.
  const n = note && firstLine(note) !== who ? ` — ${firstLine(note)}` : '';
  switch (list) {
    case 'pending': return `Queued: ${who}${n}`;
    case 'exceptions': return `Parked: ${who}${n}`;
    case 'landings': return `Landed: ${who}${n}`;
    case 'ingress': return `Ingress opened: ${note ? firstLine(note) : r.subject}`;
    default: return `Filed under ${list}: ${who}${n}`;
  }
}

/** toolDetail is what a tool call is doing, without repeating its name: the
 *  adapter titles a read "Read <path>" and a shell call by its command line. */
export function toolDetail(f) {
  const t = String(f.text ?? '').trim();
  if (!t || t === f.tool) return '';
  const re = new RegExp('^' + String(f.tool ?? '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s+', 'i');
  return clip(t.replace(re, ''), 300);
}

/** itemOf is the work item a record is about, if it is about one. */
export function itemOf(raw) {
  const r = norm(raw);
  if (/^wi-/.test(r.subject)) return r.subject;
  const m = r.scope.match(/^workitem\/(wi-[^/]+)$/);
  return m ? m[1] : '';
}
