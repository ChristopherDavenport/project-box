// Package acp drives an agent process over the Agent Client Protocol —
// newline-delimited JSON-RPC 2.0 on stdio — and is the enforcement point for
// tool policy.
//
// The stream is the only place policy can be enforced without patching the
// agent: every tool call either asks permission here (and is answered from
// [agent.ToolPolicy], not by a human) or at least announces itself as a
// session update. A denied tool that ASKS is refused; a denied tool that
// APPEARS anyway — an agent running in a mode that never asks — kills the
// session. Advisory enforcement that logs and carries on would be no
// enforcement at all.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
)

// ErrPolicyViolation reports that a denied tool appeared on the stream as
// anything other than a refusable permission request. The session is dead by
// the time this returns; a runtime that let the call finish first would be
// documenting violations, not preventing them.
var ErrPolicyViolation = errors.New("acp: denied tool appeared on the stream — session killed")

// ErrProtocolDialect reports that the agent answered initialize with a
// protocol version this runtime does not speak.
//
// Fail closed, because the failure mode of guessing is silent. A dialect
// drifted once already: v0.70's update shapes did not match the tool_call
// form this runtime counts, so tool-call metrics read 0 across all eight runs
// of the continuity experiment and the only reason anyone knew was a human
// reading a column of zeros. The same shapes carry policy enforcement, so a
// dialect this runtime cannot parse is not merely an undercount — it is a
// stream whose denied tools may never be recognised at all.
var ErrProtocolDialect = errors.New("acp: agent speaks a protocol version this runtime does not")

// ErrTokenCeiling reports that a single run spent past its per-run bound and
// was killed at the stream.
//
// The window budget could not do this. [capacity.Meter.Acquire] is a gate on
// STARTING a run: it asks whether the window has room and then has no further
// say, so the ceiling it enforces is really "the limit, plus whatever one run
// can spend before it stops on its own". Measured, that was not a rounding
// error — a 6,000,000-token window was carried to 25,419,130 by one session
// that ran 110 tool calls for 21 minutes and cost $13.60. It broke no rule;
// there was no rule to break.
//
// So the bound belongs where the spend is visible as it happens, which is the
// same place tool policy is enforced and for the same reason: the stream is
// the only vantage point that exists while the run is still running.
var ErrTokenCeiling = errors.New("acp: run exceeded its token ceiling — session killed")

// protocolVersion is the dialect this runtime is written against, offered at
// initialize and required back. Bump it only together with the update shapes
// below.
const protocolVersion = 1

// knownUpdates is every session update kind the protocol defines, and so
// every kind this runtime understands. The set is explicit so that an unknown
// kind is a fact rather than a default branch: see [Result.UnknownUpdates].
//
// It is the protocol's whole SessionUpdate union, taken from the schema the
// pinned adapter ships against (@agentclientprotocol/sdk, types.gen.d.ts, as
// of claude-agent-acp 0.70.0), not the subset a previous version of this
// file happened to handle. A kind the protocol added and this list lacks
// reads as drift, which is the right alarm; a kind this list has and the
// protocol dropped is a stale line here, which the conformance replay shows.
var knownUpdates = []string{
	"agent_message_chunk", "agent_thought_chunk", "available_commands_update",
	"config_option_update", "current_mode_update", "plan", "plan_removed",
	"plan_update", "session_info_update", "tool_call", "tool_call_update",
	"usage_update", "user_message_chunk",
}

// ErrSessionConfig reports a session configuration the agent would not take
// as asked: a model it does not list, a value it answered with something
// else, an option it does not have. Fail closed, for the reason set_mode
// does. A run on a model nobody chose is exactly the silence the session
// record exists to end, and refusing a prompt is cheaper than billing one.
var ErrSessionConfig = errors.New("acp: session configuration refused")

// ErrResume reports that continuing a prior session did not happen: the
// agent does not advertise the loadSession capability, session/load itself
// errored, or the mode/configuration this run asks for was refused on the
// loaded session. All three wrap this one sentinel so a caller does not have
// to distinguish them to do the one thing any of them calls for — fall back
// to a cold session rather than fail the build outright.
var ErrResume = errors.New("acp: session resume failed")

// ErrAborted reports that the transport dropped session/prompt after it was
// sent: an rpc error mid-response, the child dying before it answered, or the
// connection reading io.ErrUnexpectedEOF. Distinct from [Result.Prompted]
// false, which is a known zero spent on nothing — this is a known SOMETHING,
// unpriced because nothing ever said the number, and distinct again from a
// silent success that reports no usage, which is still unknown and still
// stops the box (issue #69). A caller retries on this sentinel rather than
// treating it as the box's own kill, which [ErrPolicyViolation] and
// [ErrTokenCeiling] already are.
var ErrAborted = errors.New("acp: transport dropped the call after the prompt was sent")

// UpdateKind names what the stream said an agent did. The set is closed and
// small on purpose: a live view is a timeline, not a log dump, and every kind
// here is something an operator can act on.
type UpdateKind string

const (
	// UpdateStarted is the run beginning. Emitted by [Builder] rather than by
	// the stream, and worth its own kind for a reason beyond tidiness: it is
	// what makes an empty timeline mean "this agent has done nothing yet"
	// instead of "nobody here is watching this run".
	UpdateStarted UpdateKind = "started"
	// UpdateMessage is what the agent said — the run of message chunks that
	// preceded whatever it did next.
	UpdateMessage UpdateKind = "message"
	// UpdateTool is one distinct tool call, deduped by call id exactly as the
	// settled tally is.
	UpdateTool UpdateKind = "tool"
	// UpdateDenied is a permission request policy refused at the stream.
	UpdateDenied UpdateKind = "denied"
	// UpdateEscalate and UpdateHandoff are the box verbs, carrying the agent's
	// own reason.
	UpdateEscalate UpdateKind = "escalate"
	UpdateHandoff  UpdateKind = "handoff"
)

// Update is one thing that happened on the stream, reported WHILE it is
// happening.
//
// The Result below is the same run's account, and it is only available once
// the run is over. That is the whole gap: an agent run is minutes to tens of
// minutes, and for all of it the only thing anybody outside could learn was
// that the item was claimed. These are the same facts, earlier — the ones
// carried here are carried in Result too, so a viewer that watched a run live
// and an auditor that reads the chain afterwards are told the same story.
type Update struct {
	Kind UpdateKind
	// Tool is the call's name for UpdateTool and UpdateDenied, empty exactly
	// when the dialect gave nothing to match on — which is a fact about
	// enforcement and must not be papered over with a placeholder here.
	Tool string
	// Call is the call's id on the wire, for [UpdateTool] only. The adapter
	// announces a call before it knows what the call is doing, and titles it
	// on a later update; both carry the id, so a watcher can tell one call
	// said twice from two calls.
	Call string
	// Text is the agent's message for UpdateMessage, the handoff's notes
	// for UpdateHandoff, and the call's human title for UpdateTool — what
	// the tool is doing, as against Tool, which is its name.
	Text string
	// Reason is the agent's own words for the two box verbs.
	Reason string
	// Session is the agentref (bundle.Ref) of the run that produced this
	// update — the session's name on every frame. Stamped by each role's
	// progress wrapper, not by Runtime, so a resumed continuation's own
	// bundle names its own frames rather than inheriting the run's first one.
	Session string
}

// Usage is what a run cost, as the agent reported it.
//
// Reported is the load-bearing field. An agent that says nothing about cost
// has not run for free, and a budget that treats silence as zero is the
// tool-count bug again with money attached — so absence is carried
// explicitly rather than collapsing into a zero total.
type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	CachedReadTokens  int64
	CachedWriteTokens int64
	// Tokens is the agent's own total when it gave one.
	Tokens int64
	// CostUSD is what the agent says the turn cost in money, from the
	// usage_update notification. Cumulative for the turn, not a delta, so it
	// is taken rather than summed. Zero means the agent never said.
	CostUSD float64
	// Reported is false when nothing on the stream said what the run cost.
	Reported bool
	// ContextUsed is the most tokens the agent reported in context at once,
	// and ContextSize the window it reported them against — both from
	// usage_update, which the protocol defines as exactly that pair. Together
	// they explain a cache-read figure: a run that re-reads 200K tokens a
	// turn and one that re-reads 20K are identical in the four counters
	// above and differ tenfold on the invoice. The dogfood's 25M-token run
	// averaged 206K a call on a million-token window; nothing on its record
	// could say so.
	ContextUsed int64
	ContextSize int64
	// RateLimit is the last account the agent gave of the ACCOUNT's limits —
	// status, which window, how full, when it resets — when the dialect
	// carried one; nil when nothing said. This is the signal the factory's
	// own meter cannot produce: a subscription's window is shared with
	// whoever else is using the account, and the meter sees only its half.
	RateLimit *RateLimit
	// Aborted is true when the transport dropped session/prompt after it was
	// sent — see [ErrAborted]. It rides Usage rather than Result because
	// [Builder.Spend]'s signature is func(ctx, run string, u Usage) and that
	// is the only path to the meter: a flag anywhere else would never reach
	// capacity.accrue, which is what turns this into a known zero instead of
	// an unmetered one.
	Aborted bool
}

// Credential is which account this usage says a run spent against: [CredentialPlan]
// for a subscription, [CredentialKey] for a metered API key, or "" when
// neither was said.
//
// A reported account window wins over a reported price, the same order
// capacity.Spend.Credential applies to the meter's own charge — duplicated
// rather than imported, because this package deliberately knows nothing
// about budgets (see daemon.SpendOf); internal/daemon, which imports both,
// carries a test asserting the two derivations agree.
type Credential string

const (
	CredentialPlan Credential = "plan"
	CredentialKey  Credential = "key"
)

// Credential derives which account this usage spent against.
func (u Usage) Credential() Credential {
	switch {
	case u.RateLimit != nil:
		return CredentialPlan
	case u.CostUSD > 0:
		return CredentialKey
	default:
		return ""
	}
}

// RateLimit is the account's rate-limit state as the agent reported it. The
// adapter forwards it under _meta on usage_update; the protocol reserves
// _meta for exactly this kind of agent-specific fact.
type RateLimit struct {
	// Status is allowed, allowed_warning, or rejected.
	Status string `json:"status"`
	// Window names the limit: five_hour, seven_day, and the model-specific
	// weekly windows.
	Window string `json:"window"`
	// Utilization is how much of the window is used, 0 to 1.
	Utilization float64 `json:"utilization"`
	// ResetsAt is when the window turns, in unix seconds.
	ResetsAt int64 `json:"resets_at"`
}

// Session is the agent's own account of the session it opened: the modes it
// offers and the one in force, the configuration options it exposes with
// their current values, and the commands — skills, slash commands — it made
// available. All of it the protocol's; none of it this runtime's guess.
//
// These answer the questions a run's cost and conduct turn on and which the
// agentref, being a digest of what was ASKED, cannot: which model actually
// ran, at what effort, and whether the skills the bundle granted were offered
// at all. Round 4 of the dogfood could answer none of them from its record —
// the model was whatever the machine's settings said, and a granted skill the
// agent never invoked looked identical to one it never received.
type Session struct {
	// Opened is true once session/new answered. A Result from a run that
	// failed before that carries nothing here, and says so.
	Opened bool
	Modes  ModeState
	Config []ConfigOption
	// Commands are the names the agent listed on available_commands_update,
	// in the order given. Plugin skills arrive as plugin:skill.
	Commands []string
	// ID is the ACP session id: session/new's sessionId, or, on a resume,
	// the id [Runtime.Resume] asked to continue. It identifies a
	// transcript, never a credential — an agent-session record carries it
	// so a later run can find and continue this one.
	ID string
}

// ModeState is the protocol's session mode state: the mode in force and the
// modes the agent offers.
type ModeState struct {
	Current   string   `json:"current"`
	Available []string `json:"available"`
}

// ConfigOption is one session configuration option as the agent reported
// it. Category is the protocol's semantic tag — "mode", "model",
// "thought_level", or an agent's own — and is what a bundle's Model is
// matched on, since ids are the agent's to choose.
type ConfigOption struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Category string `json:"category,omitempty"`
	// Type is select or boolean.
	Type string `json:"type,omitempty"`
	// Current is the value in force; a boolean renders as true/false.
	Current string `json:"current"`
	// Values are the selectable values in the order offered, groups
	// flattened. Empty for a boolean.
	Values []string `json:"values,omitempty"`
}

// Option finds an option by id; nil when the agent offers none by that name.
func (s Session) Option(id string) *ConfigOption {
	for i := range s.Config {
		if s.Config[i].ID == id {
			return &s.Config[i]
		}
	}
	return nil
}

// ByCategory finds the first option the agent tagged with a protocol
// category; nil when none is.
func (s Session) ByCategory(category string) *ConfigOption {
	for i := range s.Config {
		if s.Config[i].Category == category {
			return &s.Config[i]
		}
	}
	return nil
}

// Model is the model in force, from the option the agent tagged as such —
// or named "model", for an agent that tags nothing. Empty when the agent
// exposes no model at all, which is itself a fact worth the record.
func (s Session) Model() string {
	if o := s.ByCategory("model"); o != nil {
		return o.Current
	}
	if o := s.Option("model"); o != nil {
		return o.Current
	}
	return ""
}

func (s Session) ids() []string {
	ids := make([]string, 0, len(s.Config))
	for _, o := range s.Config {
		ids = append(ids, o.ID)
	}
	return ids
}

// Total is every token the run is billed for, cache reads included — cheaper,
// never free. The agent's own tally wins when it gave one: a sum of the parts
// this runtime happens to recognise is the weaker claim of the two.
func (u Usage) Total() int64 {
	if u.Tokens > 0 {
		return u.Tokens
	}
	return u.InputTokens + u.OutputTokens + u.CachedReadTokens + u.CachedWriteTokens
}

func (u *Usage) add(w wireUsage) {
	u.InputTokens += w.InputTokens
	u.OutputTokens += w.OutputTokens
	u.CachedReadTokens += w.CachedReadTokens
	u.CachedWriteTokens += w.CachedWriteTokens
	u.Tokens += w.TotalTokens
	u.Reported = true
}

// Runtime runs one agent process per call.
type Runtime struct {
	// Command is the agent argv, e.g. {"claude-agent-acp"}, optionally with a
	// confining wrapper in front of it; [confine.WorkspacePlaceholder] and
	// [confine.ContextPlaceholder] are substituted throughout. The permission
	// mode the resolver chose is applied through the protocol's
	// session/set_mode after session/new — no flags, and fail-closed: an agent
	// that refuses the mode does not get a prompt.
	Command []string
	// Dir is the working directory for the agent process: the workspace.
	Dir string
	// ContextDir is the run's materialized context set, which a confining
	// wrapper must be told about or it confines the agent out of its own
	// granted reading material. Empty is only valid for an argv that never
	// names the placeholder; [confine.Substitute] refuses the other case
	// rather than mounting nothing under a name that promised something.
	ContextDir string
	// Env carries this runtime's own explicit name=value assignments — the
	// test rig sets ACP_FAKE this way — and it always wins, even over a name
	// [Runtime.EnvNames] also lists: a value stated here is the caller
	// choosing exactly what the child sees for that name, and the allowlist
	// below exists to admit names the caller did NOT have to state one by
	// one.
	Env []string
	// EnvNames are additional names to forward from the daemon's own
	// environment, ON TOP of the built-in base set [buildEnv] always
	// includes — a box's harness.env, a confine block's env, and any lander
	// credential names the daemon decides an agent needs. Names only, never
	// name=value: the same refusal [confine.Profile.Env] makes, because a
	// value here would be a credential sitting in a struct literal.
	EnvNames []string
	// MaxTokens bounds what ONE run may spend before the session is killed;
	// zero means unbounded. See [ErrTokenCeiling] for why the window budget
	// cannot express this.
	//
	// Enforced against the stream's running total, so it binds only as
	// tightly as the agent reports: an adapter that says nothing until the
	// end cannot be stopped in the middle by anything short of a clock, which
	// is what build_timeout is for.
	MaxTokens int64
	// Transcript, when set, receives every frame that crosses the wire in
	// either direction, one JSON object per line: {"from":…,"frame":…}.
	//
	// This is how the pinned dialect is refreshed. The shapes below are
	// asserted by replaying a recorded transcript (see testdata/), and a pin
	// nobody can re-record is one that rots into fiction — which would repeat
	// the failure it exists to catch, since the last drift was found by a
	// human reading a column of zeros rather than by anything in this package.
	// Recording is off by default: a transcript carries the prompt and the
	// agent's output, so it is run material, not a log.
	Transcript io.Writer
	// Progress, when set, receives the run's [Update]s as the stream produces
	// them. Nil is the default and costs nothing.
	//
	// This is the fan-in a live view needs and nothing else here provides. The
	// Transcript above is the whole wire and is off by default because it is
	// run material; the Result is complete and arrives only at the end. In
	// between, the stream is the only thing that knows what the agent is doing,
	// and it knew it the whole time.
	//
	// Called with the runtime's lock held so that a message and the action it
	// preceded cannot arrive out of order. It must therefore not block: hand
	// off to a buffer, never to a network.
	Progress func(Update)
	// SkillsDir is where this run's skills are materialized as a generated
	// plugin, beside the workspace. Set for every run whether or not the
	// bundle granted any, because it is the third path a confining wrapper is
	// told to let through — see [confine.SkillsPlaceholder].
	SkillsDir string
	// LoadedSkills is SkillsDir when something was actually written there, and
	// empty otherwise.
	//
	// Two fields rather than one because the two questions differ: the wrapper
	// needs a path that always exists, and the harness needs a plugin
	// directory that actually contains a manifest. Handing it an empty
	// directory would fail the load for a bundle that granted nothing, which
	// is the one case where nothing is the right answer.
	LoadedSkills string
	// InheritSettings keeps the harness's own configuration sources in play —
	// the machine's enabled plugins, its environment block, its permissions.
	//
	// True is the live-inheritance posture: whatever the operator has enabled
	// in the harness reaches the next build with no reload, which is usually
	// what somebody dogfooding their own factory wants. False makes the bundle
	// the sole authority: only [agent.Bundle.Plugins] load, and the CLAUDE.md
	// discovery that needs project scope is all that survives.
	//
	// Either way the plugin set is RECORDED, because the resolver reads the
	// same files the harness will. Inheriting is not the same as not knowing.
	InheritSettings bool
	// Resume, when set, is a prior run's ACP session id to CONTINUE rather
	// than open cold: Run calls session/load instead of session/new. Refused
	// before spending anything when the agent does not advertise the
	// loadSession capability, and every other resume failure — session/load
	// itself, or the set_mode/configure assertions that follow it exactly as
	// they follow a fresh session — wraps [ErrResume], so a caller can fall
	// back to a cold run instead of failing the build. Empty is the default
	// and behaves exactly as before this field existed.
	Resume string
}

// Result is what a completed run reports.
type Result struct {
	StopReason string
	// Text accumulates the agent's message chunks.
	Text string
	// ToolCalls names every tool call that appeared on the stream, permitted
	// or not, in order — the harness's stable tool name where the dialect
	// gives one, the human title otherwise. See [sessionUpdate.tool] for why
	// that preference matters to more than tidiness.
	ToolCalls []string
	// Unenforced counts tool calls that arrived with no name at all, and so
	// were never matched against policy.
	//
	// A number rather than a flag, because the flag was already there and
	// said nothing useful: "some calls could not be checked" is a caveat an
	// operator scrolls past, and "91 of 232 could not be" is the reason to
	// stop and fix the dialect. Non-zero means enforcement had a hole of
	// exactly this size.
	Unenforced int
	// Denied lists permission requests refused by policy.
	Denied []string
	// Escalated carries the agent's own reason for declining or escalating
	// the task — the "_box/escalate" extension verb. Refusal is a recorded
	// act, not a contrived failure: the caller parks the work with this
	// reason attributed to the agent.
	Escalated string
	// EscalatedNeedsSections and EscalatedPaths are the verb's structured
	// form — issue #182: the sections the agent says the change needs
	// beyond what this run already holds, and the paths it would touch in
	// them, when it can say. Both empty means the agent used the verb with
	// reason alone, or used the ESCALATE: text fallback, which carries no
	// structured form at all.
	EscalatedNeedsSections []string
	EscalatedPaths         []string
	// HandedOff carries the agent's own reason for asking mid-run for a fresh
	// session — the "_box/handoff" extension verb. Distinct from Escalated in
	// the way that matters to the caller: an escalation wants a person and
	// this wants nothing but another session, so the work goes back to the
	// queue rather than into the exception queue.
	HandedOff string
	// HandoffNotes are what the session wants its successor to know, from the
	// verb's notes parameter. They reach the seat AT THE HANDOFF rather than
	// at settle, because the item does not settle.
	HandoffNotes []string
	// Usage is what the run cost, when the agent said. Silence is carried as
	// Usage.Reported false, never as a zero total.
	Usage Usage
	// Session is what the agent said it was, once it had opened: modes,
	// configuration, commands. See [Session].
	Session Session
	// Prompted is true once session/prompt was sent. A run that failed
	// before that — a session that would not open, a model it refused —
	// spent nothing, and that is a known zero rather than an unknown:
	// charging it as unmetered would stop the factory for an hour over a
	// run that never reached a model.
	Prompted bool
	// AbortReason is the transport's own account of why the call dropped,
	// set together with StopReason "aborted" — see [ErrAborted]. Empty on
	// every other stop.
	AbortReason string
	// UnknownUpdates lists session update kinds this runtime does not
	// understand, deduped in first-seen order, plus "tool_call:untitled" for
	// a tool call that arrived with no title for policy to match on.
	//
	// This is the drift alarm. A dialect that moves must be visible on the
	// record while it is still cheap to notice, rather than inferred later
	// from a metric that quietly read zero.
	UnknownUpdates []string
	// EnvNames is exactly the environment variable names the child process
	// was started with, sorted and deduped — never their values, so the
	// agent-session record this rides to can carry what a run could see
	// without carrying what it could use. See [Runtime.buildEnv].
	EnvNames []string
}

// baseEnvNames is forwarded to every agent process regardless of what the box
// or the daemon add — the floor an agent needs to run at all, stated once so
// the reasoning for each name lives in one place rather than in whichever
// caller happened to need it first.
var baseEnvNames = []string{
	// PATH resolves the agent binary's own subprocesses — git, language
	// toolchains, the shell a Bash tool call runs under — and an agent that
	// cannot find them fails on its first tool call, not on anything
	// policy-shaped.
	"PATH",
	// HOME is where the pinned harness keeps its model credential and
	// settings when CLAUDE_CONFIG_DIR does not override it — see
	// CLAUDE_CONFIG_DIR below for the case it does.
	"HOME",
	// CLAUDE_CONFIG_DIR is the harness config directory override;
	// [harness.ConfigDir] resolves it the same way the daemon does to
	// discover plugins, and its own doc comment says why: "a daemon and the
	// agents it spawns never disagree about which machine state is in play".
	// An agent that cannot see it reads a different ~/.claude than the one
	// the daemon just discovered plugins from.
	"CLAUDE_CONFIG_DIR",
	// LANG and TERM are what a subprocess needs to format output for the
	// locale and terminal it believes it is running in, rather than
	// guessing "C" and "dumb". LC_* is the same family and is handled below
	// by prefix, since its members are not a fixed set of names.
	"LANG", "TERM",
	// TMPDIR is where a build tool stages files outside the workspace; an
	// agent that cannot see it falls back to a hardcoded /tmp that may not
	// be writable, or may be the wrong one on a machine with several.
	"TMPDIR",
}

// buildEnv is the child's exact environment: the base set above (plus the
// daemon's own LC_* locale overrides, a family rather than a fixed name),
// [Runtime.EnvNames]' names, and finally [Runtime.Env]'s own explicit
// assignments, which always win.
//
// A name is included only when the daemon's own environment actually carries
// it. NAME=empty is never written for a name that is simply absent — that
// would tell the child "set this to nothing" where the truth is "nobody had
// an opinion", and the two are not the same instruction.
//
// The second return is the delivered names, sorted and deduped, for the
// agent-session record: it is the SAME computation that built the
// environment, not a second one that could drift from it.
func (rt *Runtime) buildEnv() ([]string, []string) {
	daemon := map[string]string{}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		daemon[name] = value
	}
	out := map[string]string{}
	take := func(name string) {
		if v, ok := daemon[name]; ok {
			out[name] = v
		}
	}
	for _, name := range baseEnvNames {
		take(name)
	}
	for name := range daemon {
		if strings.HasPrefix(name, "LC_") {
			out[name] = daemon[name]
		}
	}
	for _, name := range rt.EnvNames {
		take(name)
	}
	for _, kv := range rt.Env {
		name, value, _ := strings.Cut(kv, "=")
		out[name] = value
	}
	names := slices.Sorted(maps.Keys(out))
	env := make([]string, 0, len(out))
	for _, name := range names {
		env = append(env, name+"="+out[name])
	}
	return env, names
}

// Run executes one prompt under a bundle's policy and blocks until the agent
// finishes, the context ends, or policy kills the session.
func (rt *Runtime) Run(ctx context.Context, b agent.Bundle, prompt string) (Result, error) {
	if len(rt.Command) == 0 {
		return Result{}, errors.New("acp: no agent command configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	argv, err := confine.Substitute(rt.Command, rt.Dir, rt.ContextDir, rt.SkillsDir)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = rt.Dir
	env, envNames := rt.buildEnv()
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("acp: start agent: %w", err)
	}
	defer func() {
		stdin.Close()
		cmd.Wait()
	}()

	var (
		mu        sync.Mutex
		res       = Result{EnvNames: envNames}
		violated  error
		overspent error
		// streamed accumulates per-update usage. The prompt result wins when
		// it carries a total of its own; summing both would double-count.
		streamed Usage
		// seenCalls dedupes a tool call announced as both tool_call and
		// tool_call_update. Keying on the call id rather than the shape is
		// what keeps the count right when a dialect prefers one spelling.
		seenCalls = map[string]bool{}
		// callNames is the name each call was first seen by, and lastTitle
		// the title its latest frame carried.
		callNames = map[string]string{}
		lastTitle = map[string]string{}
		// flushed is how much of res.Text has already gone out as progress. A
		// cursor rather than a second buffer, so the live account cannot drift
		// from the settled one.
		flushed int
		// Context occupancy and the account's limits, gathered from
		// usage_update as separate facts from the spend: the usage merge
		// below picks one source for the counters, and these belong to
		// neither.
		ctxUsed, ctxSize int64
		rateLimit        *RateLimit
	)
	// flush releases the message the agent has written since its last action.
	// Chunk-by-chunk would be a log dump — hundreds of frames a second saying
	// nothing on their own — and waiting for the run to end would be no live
	// view at all. What it said, then what it did, is the rhythm an operator
	// actually reads. Callers hold mu.
	flush := func() {
		if rt.Progress != nil && flushed < len(res.Text) {
			rt.Progress(Update{Kind: UpdateMessage, Text: res.Text[flushed:]})
		}
		flushed = len(res.Text)
	}
	// emit reports one action, after whatever the agent said leading up to it.
	// Callers hold mu.
	emit := func(u Update) {
		if rt.Progress == nil {
			return
		}
		flush()
		rt.Progress(u)
	}
	conn := newConn(stdin, stdout, func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session/request_permission":
			var p permissionRequest
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			ok, reason := b.Tools.Permits(p.ToolCall.Title)
			label := p.ToolCall.Title
			if ok {
				// The name check passed; a file-shaped ask still answers to
				// where, not just what — see [Runtime.judgePath].
				if pathOK, why := rt.judgePath(p.ToolCall); !pathOK {
					ok, reason, label = false, why, why
				}
			}
			telemetry.ToolCall(ctx, p.ToolCall.Title, ok)
			mu.Lock()
			if !ok {
				res.Denied = append(res.Denied, label)
				// Only the refusal, so that the live account and Result agree
				// exactly: a permitted ask announces itself as a tool_call too,
				// and emitting here as well would count it twice.
				emit(Update{Kind: UpdateDenied, Tool: label})
			}
			mu.Unlock()
			return permissionResponse(p, ok, reason), nil
		case "_box/escalate":
			// Extension verb: the agent declines or escalates on its own
			// judgement. Acknowledged always — a refusal channel that could
			// itself be refused would teach agents not to use it.
			var p struct {
				Reason        string   `json:"reason"`
				NeedsSections []string `json:"needs_sections"`
				Paths         []string `json:"paths"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			mu.Lock()
			res.Escalated = p.Reason
			res.EscalatedNeedsSections = p.NeedsSections
			res.EscalatedPaths = p.Paths
			emit(Update{Kind: UpdateEscalate, Reason: p.Reason})
			mu.Unlock()
			return map[string]any{"acknowledged": true}, nil
		case "_box/handoff":
			// Extension verb: the agent is out of room and not out of work.
			// Acknowledged always, for the reason escalate is — a channel that
			// can be refused is one agents learn not to use, and the answer
			// they fall back on is the degraded commit.
			//
			// Acknowledged, not obeyed here. Whether a fresh session is opened
			// at all is the caller's decision and bounded by it; this end of
			// the wire only carries the request.
			var p struct {
				Reason string `json:"reason"`
				Notes  string `json:"notes"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			mu.Lock()
			if p.Reason == "" {
				// Asked without saying why is still asked. Refusing the verb
				// for want of prose would teach the same lesson as refusing it
				// outright.
				p.Reason = "no reason given"
			}
			res.HandedOff = p.Reason
			res.HandoffNotes = append(res.HandoffNotes, noteLines(p.Notes)...)
			emit(Update{Kind: UpdateHandoff, Reason: p.Reason, Text: p.Notes})
			mu.Unlock()
			return map[string]any{"acknowledged": true}, nil
		case "session/update":
			var u sessionUpdate
			if err := json.Unmarshal(params, &u); err != nil {
				// A notification's error goes nowhere on the wire, and one
				// that went nowhere on the record either is how a drift hid
				// for two rounds. Counted where the result can show it.
				mu.Lock()
				res.noteUnknown("session/update:unparsed")
				mu.Unlock()
				return nil, err
			}
			if w := u.usage(); w != nil {
				mu.Lock()
				streamed.add(*w)
				over := rt.MaxTokens > 0 && overspent == nil && streamed.Total() >= rt.MaxTokens
				if over {
					overspent = fmt.Errorf("%w: %d tokens against a ceiling of %d",
						ErrTokenCeiling, streamed.Total(), rt.MaxTokens)
				}
				mu.Unlock()
				if over {
					cancel() // kills the process via CommandContext
				}
			}
			if c := u.Update.Cost; c != nil && c.Currency == "USD" {
				// Cumulative for the turn, so the last word wins; summing
				// would bill every notification over again.
				mu.Lock()
				res.Usage.CostUSD = c.Amount
				mu.Unlock()
			}
			switch u.Update.SessionUpdate {
			case "tool_call", "tool_call_update":
				// Both spellings announce the same call, so the id decides
				// whether it is new. A dialect that only ever sends the update
				// form still gets counted, and still gets judged.
				name := u.tool()
				key := u.Update.ToolCallID
				if key == "" {
					key = name
				}
				mu.Lock()
				first := !seenCalls[key]
				seenCalls[key] = true
				title := u.Update.Title
				if first {
					res.ToolCalls = append(res.ToolCalls, name)
					callNames[key] = name
					lastTitle[key] = title
					// The stable name is what policy matches; the title is
					// what the call is doing — "Read internal/api/api.go",
					// the Bash command line — and is the half a watcher
					// wants. A timeline that only said "Read" eleven times
					// in a row said nothing.
					emit(Update{Kind: UpdateTool, Call: u.Update.ToolCallID, Tool: name, Text: title})
					if name == "" {
						// Nothing to match on, so enforcement did not happen
						// here and must not be reported as if it had. Counted,
						// not merely noted: "some calls were unchecked" is a
						// caveat, "91 of 232 were" is a finding.
						res.Unenforced++
						res.noteUnknown("tool_call:unnamed")
					}
				} else if title != "" && title != lastTitle[key] {
					// The adapter announces a call before it knows what the
					// call is doing — "Read File", "Terminal" — and titles it
					// properly on the update that follows, then repeats the
					// title on the ones after. A frame for each title the
					// call has had, once per title, under the name it was
					// first seen by; the room folds them by call id and shows
					// the latest.
					lastTitle[key] = title
					emit(Update{Kind: UpdateTool, Call: u.Update.ToolCallID, Tool: callNames[key], Text: title})
				}
				mu.Unlock()
				if !first || name == "" {
					break
				}
				if ok, _ := b.Tools.Permits(name); !ok {
					telemetry.ToolCall(ctx, name, false)
					// The agent is executing a denied tool without asking.
					mu.Lock()
					violated = fmt.Errorf("%w: %q", ErrPolicyViolation, name)
					mu.Unlock()
					cancel() // kills the process via CommandContext
				}
			case "agent_message_chunk":
				mu.Lock()
				res.Text += u.text()
				mu.Unlock()
			case "usage_update":
				mu.Lock()
				if u.Update.Size > 0 {
					ctxSize = u.Update.Size
				}
				if u.Update.Used > ctxUsed {
					ctxUsed = u.Update.Used
				}
				if rl := u.rateLimit(); rl != nil {
					rateLimit = rl
				}
				mu.Unlock()
			case "current_mode_update":
				mu.Lock()
				res.Session.Modes.Current = u.Update.CurrentModeID
				mu.Unlock()
			case "config_option_update":
				mu.Lock()
				if len(u.Update.ConfigOptions) > 0 {
					res.Session.Config = configOptions(u.Update.ConfigOptions)
				}
				mu.Unlock()
			case "available_commands_update":
				names := make([]string, 0, len(u.Update.AvailableCommands))
				for _, c := range u.Update.AvailableCommands {
					names = append(names, c.Name)
				}
				mu.Lock()
				res.Session.Commands = names
				mu.Unlock()
			default:
				if !slices.Contains(knownUpdates, u.Update.SessionUpdate) {
					mu.Lock()
					res.noteUnknown(u.Update.SessionUpdate)
					mu.Unlock()
				}
			}
			return nil, nil // notification
		default:
			return nil, fmt.Errorf("acp: unhandled agent request %q", method)
		}
	})
	conn.tape = rt.Transcript
	go conn.serve()

	var initRes struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if err := conn.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
	}, &initRes); err != nil {
		return Result{}, fmt.Errorf("acp: initialize: %w", err)
	}
	// Assert the dialect before spending anything. A version we do not speak
	// is caught here, for free, rather than as a metric that reads zero and an
	// enforcement layer that may not be enforcing.
	if initRes.ProtocolVersion != protocolVersion {
		return Result{}, fmt.Errorf("%w: offered %d, agent answered %d",
			ErrProtocolDialect, protocolVersion, initRes.ProtocolVersion)
	}
	if rt.Resume != "" && !initRes.AgentCapabilities.LoadSession {
		// Refused before spending anything: the same posture the dialect
		// assertion above takes, and for the same reason — a capability we
		// assume and the agent lacks is cheaper to catch here than as a
		// session/load call that was never going to succeed.
		return Result{}, fmt.Errorf("%w: agent does not advertise the loadSession capability", ErrResume)
	}
	var newRes struct {
		SessionID     string             `json:"sessionId"`
		Modes         *wireModes         `json:"modes"`
		ConfigOptions []wireConfigOption `json:"configOptions"`
	}
	if rt.Resume != "" {
		if err := conn.call(ctx, "session/load", map[string]any{
			"sessionId": rt.Resume, "cwd": rt.Dir, "mcpServers": []any{},
			"_meta": map[string]any{"claudeCode": map[string]any{"options": rt.harnessOptions(b)}},
		}, &newRes); err != nil {
			return Result{}, fmt.Errorf("%w: session/load: %v", ErrResume, err)
		}
		// The protocol replays the conversation as session/update
		// notifications rather than answering it inline, so the id is the
		// one asked for, never one the load response carries.
		newRes.SessionID = rt.Resume
	} else {
		if err := conn.call(ctx, "session/new", map[string]any{
			"cwd": rt.Dir, "mcpServers": []any{},
			"_meta": map[string]any{"claudeCode": map[string]any{"options": rt.harnessOptions(b)}},
		}, &newRes); err != nil {
			return Result{}, fmt.Errorf("acp: session/new: %w", err)
		}
	}
	// What the agent says it is, before anything is asked of it. Under the
	// lock because the agent may already be announcing its commands — on a
	// resume, replaying the whole prior conversation before this call even
	// returns.
	mu.Lock()
	res.Session.Opened = true
	res.Session.ID = newRes.SessionID
	if newRes.Modes != nil {
		res.Session.Modes = newRes.Modes.state()
	}
	res.Session.Config = configOptions(newRes.ConfigOptions)
	mu.Unlock()
	if mode := b.Tools.PermissionMode; mode != "" {
		if err := conn.call(ctx, "session/set_mode", map[string]any{
			"sessionId": newRes.SessionID, "modeId": mode,
		}, nil); err != nil {
			// The clock running out is not the agent refusing anything —
			// only a genuine JSON-RPC error from the agent is a refusal.
			// Wrapping ErrResume here would trigger a cold-session fallback
			// against a context that is already spent, so a context error on
			// the resume path propagates as-is instead.
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return Result{}, fmt.Errorf("acp: session/set_mode %q: %w", mode, err)
			}
			// Fail closed: running under the wrong permission mode is the
			// harness-default trap this whole layer exists to prevent. On a
			// resume this wraps ErrResume rather than failing the build
			// outright — a continuation's bundle is asserted at the wire
			// again exactly as a fresh session's is, and a continuation that
			// fails this assertion falls back to a cold session rather than
			// parking the build.
			if rt.Resume != "" {
				return Result{}, fmt.Errorf("%w: agent refused permission mode %q on the resumed session: %v", ErrResume, mode, err)
			}
			return Result{}, fmt.Errorf("acp: agent refused permission mode %q: %w", mode, err)
		}
		mu.Lock()
		res.Session.Modes.Current = mode
		mu.Unlock()
	}
	// The model and the rest of the session configuration, through the
	// protocol and asserted back. The Result travels with the refusal so the
	// record can say what the agent offered instead of what was asked.
	if err := rt.configure(ctx, conn, newRes.SessionID, b, &mu, &res.Session); err != nil {
		// Copied under the lock: the agent is still announcing its commands
		// on the stream while this refusal goes back, and a Result read
		// unlocked here is the race the detector caught on the first CI run.
		mu.Lock()
		refused := res
		mu.Unlock()
		if rt.Resume != "" {
			return refused, fmt.Errorf("%w: %v", ErrResume, err)
		}
		return refused, err
	}
	var promptRes struct {
		StopReason string     `json:"stopReason"`
		Usage      *wireUsage `json:"usage,omitempty"`
		Meta       *wireMeta  `json:"_meta,omitempty"`
	}
	mu.Lock()
	res.Prompted = true
	mu.Unlock()
	err = conn.call(ctx, "session/prompt", map[string]any{
		"sessionId": newRes.SessionID,
		"prompt":    []map[string]any{{"type": "text", "text": prompt}},
	}, &promptRes)

	mu.Lock()
	defer mu.Unlock()
	// Whatever the agent said after its last action. Without this a run that
	// ends on a paragraph — an escalation explaining itself, a summary of what
	// was changed — would have its closing words appear only once the item
	// settled, which is the moment a live view stops being read.
	flush()
	// The turn's own total wins when it has one; otherwise the stream's
	// increments are the best account there is. Summing both would bill the
	// run twice.
	cost := res.Usage.CostUSD // gathered from the stream; survives whichever branch wins
	switch {
	case promptRes.Usage != nil:
		res.Usage.add(*promptRes.Usage)
		if promptRes.Usage.empty() {
			// A usage object that parses to nothing is what a rename looks
			// like from here, and it is indistinguishable from a free turn
			// unless somebody says so.
			res.noteUnknown("usage:all-zero")
		}
	case promptRes.Meta != nil && promptRes.Meta.Usage != nil:
		res.Usage.add(*promptRes.Meta.Usage)
	default:
		res.Usage = streamed
	}
	res.Usage.CostUSD = cost
	res.Usage.ContextUsed, res.Usage.ContextSize, res.Usage.RateLimit = ctxUsed, ctxSize, rateLimit
	if violated != nil {
		return res, violated
	}
	// Before the generic prompt error, which on this path is only ever the
	// kill reporting itself. The usage rides the Result either way: a run
	// stopped for spending too much is the last run whose cost may go
	// unrecorded.
	if overspent != nil {
		return res, overspent
	}
	if err != nil {
		// The prompt was sent — Prompted is already true — and the transport
		// dropped the call: round 10's "Server error mid-response", or
		// io.ErrUnexpectedEOF when the child died. Nobody decided this, so it
		// is neither of the two kills above; it is a known something whose
		// price nothing ever said, and the caller retries it rather than
		// treating it as a build the box refused.
		res.StopReason = "aborted"
		res.AbortReason = err.Error()
		res.Usage.Aborted = true
		return res, fmt.Errorf("%w: %v", ErrAborted, err)
	}
	res.StopReason = promptRes.StopReason
	return res, nil
}

// noteLines splits a handoff's notes into one claim per line. The verb takes
// prose because that is what an agent has; the seat stores claims, and a wall
// of text is one claim nobody can supersede or mark stale independently.
func noteLines(s string) []string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// harnessOptions is the bundle's agentic surface in the shape the adapter
// forwards to its harness: `_meta.claudeCode.options` on session/new, which
// claude-agent-acp passes through to the underlying SDK.
//
// This is the seam that was missing. Bundle.Skills and Bundle.MCP have existed
// since Phase 2 — resolved, merged, hashed into the agentref, rendered by
// Explain — and were delivered nowhere: session/new went out with an empty
// mcpServers array and no options at all, so a bundle could grant a skill and
// the agent would never see it. Meanwhile the adapter's own default loaded the
// machine's ENTIRE harness configuration, which is the opposite failure: the
// factory recorded a set it did not deliver, and delivered a set it did not
// record.
//
// strictMcpConfig is unconditional. It is the one option here that is a
// security property rather than a preference: without it a plugin's own
// .mcp.json is loaded with the plugin, so inheriting a machine's knowledge
// means inheriting its credentials — an unattended builder holding live Jira,
// Slack and GitHub write tools that no box asked for. With it, the only MCP
// servers that exist are the ones [agent.Bundle.MCP] names, which is a
// decision somebody made and evidence can show.
func (rt *Runtime) harnessOptions(b agent.Bundle) map[string]any {
	// Every plugin is loaded with skipMcpDiscovery, belt to strictMcpConfig's
	// braces: one bounds what the session may connect to, the other bounds
	// what the plugin loader even reads.
	plugins := make([]map[string]any, 0, len(b.Plugins)+1)
	if !rt.InheritSettings {
		for _, p := range b.Plugins {
			plugins = append(plugins, map[string]any{
				"type": "local", "path": p.Path, "skipMcpDiscovery": !p.MCP,
			})
		}
	}
	if rt.LoadedSkills != "" {
		plugins = append(plugins, map[string]any{
			"type": "local", "path": rt.LoadedSkills, "skipMcpDiscovery": true,
		})
	}

	mcp := map[string]any{}
	for _, m := range b.MCP {
		switch {
		case m.URL != "":
			mcp[m.Name] = map[string]any{"type": "http", "url": m.URL}
		case len(m.Command) > 0:
			mcp[m.Name] = map[string]any{
				"type": "stdio", "command": m.Command[0], "args": m.Command[1:],
			}
		}
	}

	// project scope is not optional even in the locked-down posture: the SDK
	// loads CLAUDE.md only when project settings are in play, and a builder
	// that cannot read the repository's own conventions is a builder that will
	// violate them.
	sources := []string{"project"}
	if rt.InheritSettings {
		sources = []string{"user", "project", "local"}
	}

	opts := map[string]any{
		"settingSources":  sources,
		"strictMcpConfig": true,
		"mcpServers":      mcp,
	}
	if len(plugins) > 0 {
		opts["plugins"] = plugins
	}
	return opts
}

// configure applies the bundle's session configuration through the protocol
// — the model first, matched on the option the agent tags with the model
// category, then every option the bundle names by id — and asserts each one
// back from the agent's own answer.
//
// The assertion is exact. An agent that resolves an alias to something it
// prefers has changed the agent on the record, so the operator names the
// value the agent lists and the record shows the list. Refusing costs a
// session/new; running the wrong model costs a run.
func (rt *Runtime) configure(ctx context.Context, conn *conn, sessionID string, b agent.Bundle, mu *sync.Mutex, s *Session) error {
	set := func(opt ConfigOption, want string) error {
		var value any = want
		if opt.Type == "boolean" {
			v, err := strconv.ParseBool(want)
			if err != nil {
				return fmt.Errorf("%w: %s is a boolean option and %q is not a boolean", ErrSessionConfig, opt.ID, want)
			}
			value, want = v, strconv.FormatBool(v)
		}
		var setRes struct {
			ConfigOptions []wireConfigOption `json:"configOptions"`
		}
		if err := conn.call(ctx, "session/set_config_option", map[string]any{
			"sessionId": sessionID, "configId": opt.ID, "value": value,
		}, &setRes); err != nil {
			return fmt.Errorf("%w: agent refused %s=%q (it offers %v): %v", ErrSessionConfig, opt.ID, want, opt.Values, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if setRes.ConfigOptions != nil {
			s.Config = configOptions(setRes.ConfigOptions)
		}
		got := s.Option(opt.ID)
		if got == nil {
			return fmt.Errorf("%w: asked for %s=%q and the agent's answer no longer lists %s at all", ErrSessionConfig, opt.ID, want, opt.ID)
		}
		if got.Current != want {
			return fmt.Errorf("%w: asked for %s=%q, agent answered %s=%q (it offers %v)", ErrSessionConfig, opt.ID, want, opt.ID, got.Current, opt.Values)
		}
		return nil
	}
	if b.Model != "" {
		mu.Lock()
		opt := s.ByCategory("model")
		if opt == nil {
			opt = s.Option("model")
		}
		var chosen ConfigOption
		if opt != nil {
			chosen = *opt
		}
		ids := s.ids()
		mu.Unlock()
		if opt == nil {
			return fmt.Errorf("%w: model %q requested and the agent offers no model option (its options: %v)", ErrSessionConfig, b.Model, ids)
		}
		if err := set(chosen, b.Model); err != nil {
			return err
		}
	}
	for _, id := range slices.Sorted(maps.Keys(b.Session)) {
		mu.Lock()
		opt := s.Option(id)
		var chosen ConfigOption
		if opt != nil {
			chosen = *opt
		}
		ids := s.ids()
		mu.Unlock()
		if opt == nil {
			return fmt.Errorf("%w: session option %q requested and the agent offers none by that id (its options: %v)", ErrSessionConfig, id, ids)
		}
		if err := set(chosen, b.Session[id]); err != nil {
			return err
		}
	}
	return nil
}

// noteUnknown records a dialect surprise once. Callers hold mu.
func (r *Result) noteUnknown(kind string) {
	if kind == "" {
		kind = "(empty)"
	}
	if !slices.Contains(r.UnknownUpdates, kind) {
		r.UnknownUpdates = append(r.UnknownUpdates, kind)
	}
}

type toolCall struct {
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title"`
	Kind       string          `json:"kind"`
	RawInput   json.RawMessage `json:"rawInput"`
	Locations  []struct {
		Path string `json:"path"`
	} `json:"locations"`
	// Meta carries the harness's own account of the call, the same shape
	// [sessionUpdate.tool] reads it from — see [wireMeta.ClaudeCode]'s doc
	// comment for why toolName is the identifier and Title is prose that
	// cannot be matched or judged.
	Meta *wireMeta `json:"_meta,omitempty"`
}

// name is this call's stable identifier the same way [sessionUpdate.tool]
// prefers it: the harness's toolName when the dialect supplies one, the
// human title otherwise.
func (t toolCall) name() string {
	if t.Meta != nil && t.Meta.ClaudeCode != nil && t.Meta.ClaudeCode.ToolName != "" {
		return t.Meta.ClaudeCode.ToolName
	}
	return t.Title
}

// paths is every path this call's rawInput or locations name, in the fields
// the pinned dialect actually uses: file_path (Read/Edit/Write/MultiEdit),
// path (Glob/Grep), notebook_path (NotebookEdit), and locations[].path.
func (t toolCall) paths() []string {
	var in struct {
		FilePath     string `json:"file_path"`
		Path         string `json:"path"`
		NotebookPath string `json:"notebook_path"`
	}
	if len(t.RawInput) > 0 {
		json.Unmarshal(t.RawInput, &in)
	}
	var out []string
	for _, p := range []string{in.FilePath, in.Path, in.NotebookPath} {
		if p != "" {
			out = append(out, p)
		}
	}
	for _, l := range t.Locations {
		if l.Path != "" {
			out = append(out, l.Path)
		}
	}
	return out
}

type permissionRequest struct {
	SessionID string   `json:"sessionId"`
	ToolCall  toolCall `json:"toolCall"`
	Options   []struct {
		OptionID string `json:"optionId"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

// pathJudgedTools are the tool names whose rawInput is judged by path, on top
// of whatever [agent.ToolPolicy.Permits] already decided by name. Bash is
// deliberately absent: its rawInput carries a shell command line, not a
// path, and pretending a command can be judged by path would be a boundary
// that looks enforced and is not — the container is Bash's boundary, per
// ARCHITECTURE's ingress section, and this check is defense inside it.
var pathJudgedTools = map[string]bool{
	"Read": true, "Edit": true, "Write": true, "MultiEdit": true,
	"NotebookEdit": true, "Glob": true, "Grep": true,
}

// judgePath answers a file-shaped permission ask by where it reads or
// writes, not just what it is named — the gap issue #73 found: a builder
// whose workspace sits inside the operator's own checkout read and wrote it
// by absolute path, and nothing judged that because tool policy only ever
// matched names.
//
// The boundary is three subtrees, not one: rt.Dir (the workspace),
// rt.ContextDir (the materialized context set), and rt.SkillsDir (the
// generated skills plugin) — the latter two are SIBLINGS of the workspace,
// not under it, so each is checked on its own.
//
// A tool outside [pathJudgedTools] is left to the name check alone — Bash
// among them, see that var's comment. A judged tool with no path anywhere
// parseable is refused rather than waved through, except Glob/Grep, whose
// path is optional and defaults to the cwd. rt.Dir empty means a bare
// Runtime (tests) with no boundary to assert, so judging is skipped.
func (rt *Runtime) judgePath(tc toolCall) (bool, string) {
	name := tc.name()
	if !pathJudgedTools[name] || rt.Dir == "" {
		return true, ""
	}
	paths := tc.paths()
	if len(paths) == 0 {
		if name == "Glob" || name == "Grep" {
			return true, ""
		}
		return false, fmt.Sprintf("%s: no path to judge", name)
	}
	for _, p := range paths {
		if !rt.pathInBounds(p) {
			return false, fmt.Sprintf("%s %s", name, p)
		}
	}
	return true, ""
}

// pathInBounds resolves p against the workspace when it is relative, then
// asks whether it falls under rt.Dir, rt.ContextDir, or rt.SkillsDir — a
// separator-aware prefix check, so "/a/b-evil" is never mistaken for a path
// under "/a/b".
func (rt *Runtime) pathInBounds(p string) bool {
	if !filepath.IsAbs(p) {
		p = filepath.Join(rt.Dir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, root := range []string{rt.Dir, rt.ContextDir, rt.SkillsDir} {
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootAbs = filepath.Clean(rootAbs)
		if abs == rootAbs || strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// permissionResponse picks the agent's own allow/reject option matching the
// policy decision, falling back to a cancelled outcome when the agent offered
// nothing suitable — never to an allow.
func permissionResponse(p permissionRequest, allow bool, reason string) any {
	want := "reject"
	if allow {
		want = "allow"
	}
	for _, o := range p.Options {
		if len(o.Kind) >= len(want) && o.Kind[:len(want)] == want {
			return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": o.OptionID}}
		}
	}
	return map[string]any{"outcome": map[string]any{"outcome": "cancelled", "reason": reason}}
}

type sessionUpdate struct {
	SessionID string `json:"sessionId"`
	Update    struct {
		SessionUpdate string `json:"sessionUpdate"`
		ToolCallID    string `json:"toolCallId"`
		Title         string `json:"title"`
		// Content is one shape on a message chunk — an object with the text
		// — and another on a tool call: a list of what the call produced.
		// Decoded lazily by text(), because a field typed for the first
		// shape made the decoder refuse the second, and the handler then
		// dropped the whole event: every titled tool call the adapter sent
		// with content, which is most of them, and every completion.
		Content json.RawMessage `json:"content"`
		// Cost rides usage_update, and is the only place the agent says what
		// a turn cost in money rather than tokens. Cumulative for the turn.
		Cost *struct {
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		} `json:"cost,omitempty"`
		// Used and Size ride usage_update: tokens in context, and the window.
		Used int64 `json:"used"`
		Size int64 `json:"size"`
		// CurrentModeID rides current_mode_update; ConfigOptions rides
		// config_option_update as the full set; AvailableCommands rides
		// available_commands_update.
		CurrentModeID     string             `json:"currentModeId"`
		ConfigOptions     []wireConfigOption `json:"configOptions"`
		AvailableCommands []struct {
			Name string `json:"name"`
		} `json:"availableCommands"`
		Meta *wireMeta `json:"_meta,omitempty"`
	} `json:"update"`
	Meta *wireMeta `json:"_meta,omitempty"`
}

// text is the chunk's text when content is the message-chunk object, and
// empty for every other shape content takes.
func (u sessionUpdate) text() string {
	var c struct {
		Text string `json:"text"`
	}
	if len(u.Update.Content) == 0 || u.Update.Content[0] != '{' {
		return ""
	}
	if err := json.Unmarshal(u.Update.Content, &c); err != nil {
		return ""
	}
	return c.Text
}

// rateLimit finds the account's limit report on an update, at either place
// the dialect may hang metadata. Nil when the update carried none.
func (u sessionUpdate) rateLimit() *RateLimit {
	for _, m := range []*wireMeta{u.Update.Meta, u.Meta} {
		if m != nil && m.RateLimit != nil {
			w := m.RateLimit
			return &RateLimit{Status: w.Status, Window: w.RateLimitType,
				Utilization: w.Utilization, ResetsAt: int64(w.ResetsAt)}
		}
	}
	return nil
}

// wireModes is the protocol's SessionModeState as it arrives on session/new.
type wireModes struct {
	CurrentModeID  string `json:"currentModeId"`
	AvailableModes []struct {
		ID string `json:"id"`
	} `json:"availableModes"`
}

func (m wireModes) state() ModeState {
	s := ModeState{Current: m.CurrentModeID}
	for _, a := range m.AvailableModes {
		s.Available = append(s.Available, a.ID)
	}
	return s
}

// wireConfigOption is the protocol's SessionConfigOption. currentValue is a
// string for a select and a bool for a boolean, so it is read raw and
// rendered once; options are either values or groups of values, so the same
// shape reads both and flattens.
type wireConfigOption struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Category     string             `json:"category"`
	Type         string             `json:"type"`
	CurrentValue json.RawMessage    `json:"currentValue"`
	Options      []wireSelectOption `json:"options"`
}

type wireSelectOption struct {
	Value   string             `json:"value"`
	Options []wireSelectOption `json:"options"` // present on a group
}

func (o wireConfigOption) current() string {
	var s string
	if json.Unmarshal(o.CurrentValue, &s) == nil {
		return s
	}
	var b bool
	if json.Unmarshal(o.CurrentValue, &b) == nil {
		return strconv.FormatBool(b)
	}
	return ""
}

func configOptions(ws []wireConfigOption) []ConfigOption {
	out := make([]ConfigOption, 0, len(ws))
	for _, w := range ws {
		o := ConfigOption{ID: w.ID, Name: w.Name, Category: w.Category, Type: w.Type, Current: w.current()}
		for _, v := range w.Options {
			if len(v.Options) > 0 {
				for _, g := range v.Options {
					o.Values = append(o.Values, g.Value)
				}
				continue
			}
			o.Values = append(o.Values, v.Value)
		}
		out = append(out, o)
	}
	return out
}

// tool names a call the way policy and the tally both want it named: the
// harness's stable tool name when the dialect supplies one, the human title
// otherwise, and empty when neither — which is a fact the caller must record
// rather than paper over, because it means nothing was matched.
func (u sessionUpdate) tool() string {
	for _, m := range []*wireMeta{u.Update.Meta, u.Meta} {
		if m != nil && m.ClaudeCode != nil && m.ClaudeCode.ToolName != "" {
			return m.ClaudeCode.ToolName
		}
	}
	return u.Update.Title
}

// usage finds a cost report on an update, at either of the two places the
// dialect may hang it. Both spellings are part of the pinned dialect: an
// agent that reports somewhere else reads as unmetered — loudly — rather
// than as free.
func (u sessionUpdate) usage() *wireUsage {
	if u.Update.Meta != nil && u.Update.Meta.Usage != nil {
		return u.Update.Meta.Usage
	}
	if u.Meta != nil && u.Meta.Usage != nil {
		return u.Meta.Usage
	}
	return nil
}

type wireMeta struct {
	Usage *wireUsage `json:"usage,omitempty"`
	// RateLimit is claude-agent-acp's forwarding of the harness's
	// rate_limit_event, on usage_update, under a key the adapter owns.
	RateLimit *struct {
		Status        string  `json:"status"`
		ResetsAt      float64 `json:"resetsAt"`
		RateLimitType string  `json:"rateLimitType"`
		Utilization   float64 `json:"utilization"`
	} `json:"_claude/rateLimit,omitempty"`
	// ClaudeCode carries the harness's own account of a tool call. toolName is
	// the STABLE identifier — "Bash", "Edit", "Read" — as against title, which
	// is prose written for a human to read and for Bash is the whole command
	// line.
	//
	// That distinction turned out to be the difference between a tool policy
	// and a decoration. Permits is an exact match, so a deny list naming
	// "Bash" never fired against a title of `cd /Users/... && find . -name
	// '*.go'`, and the tally keyed on the same string grew a new entry per
	// command — 46 "distinct tools" in one run of 53 calls. Meanwhile
	// tool_call_update carries no title at all, and the enforcement path
	// skipped those outright: measured across four real runs, 91 of 232 calls
	// went unmatched for want of anything to match on.
	ClaudeCode *struct {
		ToolName string `json:"toolName"`
	} `json:"claudeCode,omitempty"`
}

// wireUsage is the cost report as it appears on the wire, taken from the
// adapter's own source rather than guessed: claude-agent-acp's sessionUsage()
// returns exactly these five fields on the prompt response.
//
// The names matter more than they look. An earlier version of this struct
// spelled the cache counters cacheReadInputTokens / cacheCreationInputTokens —
// the Anthropic API's names, not the adapter's. Input and output parsed, the
// two cache figures silently read zero, and Reported still came back true. For
// an agent re-reading a repository the cache read is usually the largest of
// the four, so the meter would have shown a confident, plausible figure that
// was wrong by a wide margin, with nothing anywhere to say so — the exact
// failure the dialect assertions in this package exist to prevent, in the one
// place those assertions did not reach.
type wireUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	// TotalTokens is the adapter's own tally over the other four. Preferred
	// when present: it is what the agent believes it spent, and a sum of the
	// parts this runtime happens to recognise is a weaker claim.
	TotalTokens int64 `json:"totalTokens"`
}

// empty reports a usage object that parsed to nothing at all — the signature
// of the field names having moved.
func (w wireUsage) empty() bool {
	return w.InputTokens == 0 && w.OutputTokens == 0 &&
		w.CachedReadTokens == 0 && w.CachedWriteTokens == 0 && w.TotalTokens == 0
}

// conn is a minimal bidirectional JSON-RPC 2.0 endpoint over ndjson. Small on
// purpose: the protocol surface the runtime needs is three outbound calls and
// two inbound methods, and a dependency would not make the framing simpler.
type conn struct {
	w       io.Writer
	r       *bufio.Scanner
	handler func(method string, params json.RawMessage) (any, error)

	tapeMu sync.Mutex
	tape   io.Writer

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResult
}

// record writes one frame to the transcript in the direction it crossed.
// Nothing else may write to the tape, so its own lock is enough.
func (c *conn) record(from string, frame []byte) {
	c.tapeMu.Lock()
	defer c.tapeMu.Unlock()
	if c.tape == nil {
		return
	}
	// A frame that is not JSON is kept as text rather than dropped. serve
	// skips malformed lines, so the transcript would otherwise be the one
	// place a dialect emitting garbage looked clean.
	e := struct {
		From  string          `json:"from"`
		Frame json.RawMessage `json:"frame,omitempty"`
		Raw   string          `json:"raw,omitempty"`
	}{From: from}
	if json.Valid(frame) {
		e.Frame = frame
	} else {
		e.Raw = string(frame)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	c.tape.Write(append(line, '\n'))
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

func newConn(w io.Writer, r io.Reader, handler func(string, json.RawMessage) (any, error)) *conn {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	return &conn{w: w, r: sc, handler: handler, pending: map[int64]chan rpcResult{}}
}

func (c *conn) send(m rpcMessage) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Recorded before the write, not after: once the bytes are on the wire the
	// agent can answer before this goroutine gets back here, and serve records
	// on arrival. Write-then-record let a response reach the tape before the
	// request it answers; a reader replaying causally then has nothing to
	// answer yet and waits forever. Under c.mu, so a request is always on tape
	// before anything that could answer it.
	c.record("client", b)
	_, err = c.w.Write(append(b, '\n'))
	return err
}

func (c *conn) call(ctx context.Context, method string, params any, out any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan rpcResult, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(rpcMessage{ID: &id, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w waiting for %s", ctx.Err(), method)
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out != nil && r.result != nil {
			return json.Unmarshal(r.result, out)
		}
		return nil
	}
}

func (c *conn) serve() {
	for c.r.Scan() {
		line := c.r.Bytes()
		if len(line) == 0 {
			continue
		}
		c.record("agent", line)
		var m rpcMessage
		if err := json.Unmarshal(line, &m); err != nil {
			continue // a malformed line is the agent's bug; keep serving
		}
		switch {
		case m.Method != "" && m.ID != nil: // request from the agent
			go func(m rpcMessage) {
				result, err := c.handler(m.Method, m.Params)
				resp := rpcMessage{ID: m.ID}
				if err != nil {
					resp.Error = &rpcError{Code: -32603, Message: err.Error()}
				} else {
					b, _ := json.Marshal(result)
					resp.Result = b
				}
				c.send(resp)
			}(m)
		case m.Method != "": // notification
			c.handler(m.Method, m.Params)
		case m.ID != nil: // response to one of our calls
			c.mu.Lock()
			ch := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				var err error
				if m.Error != nil {
					err = m.Error
				}
				ch <- rpcResult{result: m.Result, err: err}
			}
		}
	}
	// EOF: fail every pending call so nothing blocks forever on a dead agent.
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- rpcResult{err: io.ErrUnexpectedEOF}
		delete(c.pending, id)
	}
}
