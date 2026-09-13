// Command box is the control room: the human surface of a factory whose
// execution loop has none. Exceptions, the stop button, review sampling and
// sign-off, and the audit queries — nothing here builds, gates, or lands.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"net"
	"net/http"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/boxctx"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/gateway"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("box " + version.String())
		os.Exit(0)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "box:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("box", flag.ContinueOnError)
	db := fs.String("db", "box.db", "store path (direct mode)")
	daemon := fs.String("daemon", "", "daemon API address (unix:/path or tcp:host:port); talks to boxd instead of opening the store")
	token := fs.String("token", "", "bearer token for a tcp daemon")
	tn := fs.String("tenant", "", "tenant id")
	actor := fs.String("actor", "", "X-Box-Actor on-behalf-of claim sent to a daemon (rare — the socket already knows who called); direct-store mode falls back to $USER")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 && fs.Arg(0) == "context" {
		return contextCmd(fs.Args()[1:])
	}
	if *tn == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: box -tenant <id> [-db path|-daemon addr] <status|queue|exceptions|flakes|submit|item|provenance|stop|resume|room|context> ...")
	}
	t := tenant.ID(*tn)
	if err := t.Validate(); err != nil {
		return err
	}
	// Contexts fill in what flags left empty — flags always win.
	if cx, name, ok, err := boxctx.Resolve(os.Getenv("BOX_CONTEXT")); err != nil {
		return err
	} else if ok {
		if *daemon == "" {
			*daemon = cx.Daemon
		}
		if *token == "" {
			*token = cx.Token
		}
		if *tn == "" {
			*tn = cx.Tenant
		}
		if cx.Actor != "" && *actor == "" {
			*actor = cx.Actor
		}
		_ = name
	}
	if fs.Arg(0) == "room" {
		return room(*daemon, fs.Args()[1:])
	}
	if *daemon != "" {
		return viaDaemon(*daemon, *token, *actor, fs.Arg(0), fs.Args()[1:])
	}
	s, err := sqlite.Open(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	ctx := context.Background()
	// Direct-store mode has no socket to assert a peer, so it keeps its
	// pre-existing actor behavior: an explicit -actor, or $USER.
	directActor := *actor
	if directActor == "" {
		directActor = os.Getenv("USER")
	}
	room := &control.Room{Store: s, Tenant: t, Actor: directActor}
	q := &queue.Queue{Store: s, Runner: directActor}
	rest := fs.Args()[1:]

	switch fs.Arg(0) {
	case "queue":
		sub := flag.NewFlagSet("queue", flag.ContinueOnError)
		state := sub.String("state", "", "comma-separated states (pending, claimed, waiting, parked, done, failed); empty shows all")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		entries, err := room.Queue(ctx, parseStates(*state)...)
		if err != nil {
			return err
		}
		printQueue(entries)
	case "exceptions":
		items, err := room.Exceptions(ctx)
		if err != nil {
			return err
		}
		printExceptions(items)
	case "flakes":
		items, err := room.Flakes(ctx)
		if err != nil {
			return err
		}
		printFlakes(items)
	case "stop":
		if len(rest) == 0 {
			return fmt.Errorf("stop needs a reason — it becomes the record")
		}
		if err := q.Stop(ctx, t, directActor, rest[0]); err != nil {
			return err
		}
		fmt.Println("stopped: no new claims until resume")
	case "resume":
		if len(rest) == 0 {
			return fmt.Errorf("resume needs a reason — it becomes the record")
		}
		if err := q.Resume(ctx, t, directActor, rest[0]); err != nil {
			return err
		}
		fmt.Println("resumed")
	case "sample":
		sub := flag.NewFlagSet("sample", flag.ContinueOnError)
		n := sub.Int("n", 25, "items to sample")
		seed := sub.String("seed", "", "recorded seed; same seed reproduces the same set")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		if *seed == "" {
			return fmt.Errorf("sample needs -seed — the seed is what makes the selection reproducible by an auditor")
		}
		items, err := room.Sample(ctx, *n, *seed)
		if err != nil {
			return err
		}
		for _, it := range items {
			fmt.Printf("%-24s %s  %s\n", it.ID, it.Head, it.Task)
		}
	case "signoff":
		sub := flag.NewFlagSet("signoff", flag.ContinueOnError)
		item := sub.String("item", "", "work item id")
		verdict := sub.String("verdict", "", "pass, or a finding")
		note := sub.String("note", "", "reviewer note")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		if *item == "" {
			return fmt.Errorf("signoff needs -item")
		}
		return room.SignOff(ctx, *item, *verdict, *note)
	case "provenance":
		if len(rest) == 0 {
			return fmt.Errorf("provenance needs a commit sha")
		}
		ch, err := evidence.Provenance(ctx, s.Log(), t, rest[0])
		if err != nil {
			return err
		}
		printChain(ch)
	default:
		return fmt.Errorf("unknown command %q", fs.Arg(0))
	}
	return nil
}

func parseStates(s string) []queue.State {
	var out []queue.State
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, queue.State(part))
		}
	}
	return out
}

// printQueue renders the queue the way an operator reads it: one line per
// unit, and underneath it the note and whatever the frontier has to say. The
// note is the answer to "why is it still there" — which the queue has always
// written and a counter can never carry.
//
// The frontier lines are here and not only in the browser on purpose. A fact
// some readers of the queue know and others do not is worse than one nobody
// has, because work does not disappear loudly from a view — it disappears
// plausibly, and the state vocabulary has already had that failure once.
// printSession is one unit as `box item` now answers it: what it is, how far
// it has got, and what its agent has been doing — the three things a claimed
// item could not say.
func printSession(s control.Session) {
	e := s.QueueEntry
	fmt.Printf("%s state=%s stage=%s head=%s\n", e.ID, e.State, dash(e.Stage), dash(e.Head))
	origin := e.Route
	if e.Key != "" {
		origin += "/" + e.Key
	}
	if origin == "" {
		origin = e.Ingress
	}
	fmt.Printf("  %s  box=%s repo=%s origin=%s\n", e.Task, dash(e.Box), dash(e.Repo), dash(origin))
	if e.Plan != "" {
		// The link between the origination and this unit. An operator looking
		// at six rows from one origination otherwise has no way to ask whether
		// they came from one decision or from six ticks of a decomposer.
		fmt.Printf("  kind=%s plan=%s node=%s\n", dash(e.Kind), short(strings.TrimPrefix(e.Plan, "plan-")), dash(e.Node))
	}
	if e.Seat != "" {
		fmt.Printf("  seat=%s agentref=%s\n", e.Seat, short(e.AgentRef))
	}
	for _, l := range s.Layers {
		fmt.Printf("    layer %-6s %-20s %s %s\n", l.Scope, l.Name, l.Source, short(l.Digest))
	}
	if e.Stage != "" {
		fmt.Printf("  %s in stage, %s since it opened\n", age(e.InStateSec), age(e.AgeSec))
	}
	if e.Note != "" {
		fmt.Printf("  %s\n", e.Note)
	}
	if len(s.Timeline) == 0 {
		// Silence and blindness are not the same thing, and the difference
		// decides whether an operator goes and looks at a stuck run.
		if e.State == queue.StateClaimed && !s.Watching {
			fmt.Println("  (no live stream here — another runner holds this, or the window has rolled)")
		}
		return
	}
	fmt.Println("  timeline:")
	for _, f := range s.Timeline {
		line, _, _ := strings.Cut(frameText(f), "\n")
		fmt.Printf("    %s %-8s %s\n", f.At.Format("15:04:05"), f.Kind, line)
	}
}

// frameText is the one line a frame is worth on a terminal. The kinds are the
// wire's strings rather than the runtime's constants: this is an HTTP client,
// and a CLI that imported the ACP runtime to read a JSON field would be
// coupling the two ends of a protocol that exists to keep them apart.
func frameText(f control.Frame) string {
	switch {
	case f.Reason != "":
		return f.Reason
	case f.Tool != "":
		return f.Tool
	case f.Kind == "tool":
		// A call the dialect named nothing at all: policy had nothing to match
		// on, which is a hole and not a blank.
		return "(unnamed — nothing for policy to match)"
	}
	return f.Text
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// tokenSplit renders a window's tokens by kind, omitting kinds that did not
// occur rather than printing them as zero. Fixed order rather than sorted:
// these have a rank the states do not — fresh input, what was generated, and
// then the two cache figures that are cheap and usually largest.
func tokenSplit(sp *capacity.Report) string {
	var parts []string
	for _, kv := range []struct {
		name string
		n    int64
	}{
		{"in", sp.Input}, {"out", sp.Output},
		{"cache read", sp.CacheRead}, {"cache write", sp.CacheWrite},
	} {
		if kv.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", kv.name, kv.n))
		}
	}
	return strings.Join(parts, ", ")
}

// byState renders a backlog's tally in a stable order — alphabetical, because
// the states have no rank and a map's order would make two identical statuses
// print differently.
func byState(m map[queue.State]int) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func printQueue(entries []control.QueueEntry) {
	if len(entries) == 0 {
		fmt.Println("the queue is empty")
		return
	}
	ready := 0
	for _, e := range entries {
		origin := e.Route
		if e.Key != "" {
			origin += "/" + e.Key
		}
		if origin == "" {
			origin = e.Ingress
		}
		mark := " "
		if e.Ready {
			mark, ready = ">", ready+1
		}
		fmt.Printf("%s %-22s %-8s %6s  %-22s %s\n", mark, e.ID, e.State, age(e.AgeSec), origin, e.Task)
		if e.Note != "" {
			fmt.Printf("    %s [%s in state]\n", e.Note, age(e.InStateSec))
		}
		if len(e.BlockedBy) > 0 {
			fmt.Printf("    blocked by %s\n", strings.Join(e.BlockedBy, ", "))
		}
		if e.Root != "" {
			fmt.Printf("    cascaded from %s\n", e.Root)
		}
		if e.Runner != "" {
			if e.LeaseLapsed {
				fmt.Printf("    held by %s — LEASE LAPSED %s ago, nothing is running this\n",
					e.Runner, age(-e.LeaseExpiresSec))
			} else {
				fmt.Printf("    held by %s — %s of lease left\n", e.Runner, age(e.LeaseExpiresSec))
			}
		}
	}
	// Ready work with nothing moving is the first symptom of a stopped pump or
	// a budget refusal, so the count goes next to the total rather than being
	// left for the reader to tally off the markers.
	fmt.Printf("\n%d item(s), %d ready to claim (>)\n", len(entries), ready)
}

// printTranscript renders a run so a person can judge it: what the agent was
// told, how that instruction was composed, and what it said back.
//
// The agentref settles whether two runs had the same instructions and cannot
// show you either of them, so for evaluating a prompt — as against auditing
// one — the digest is the wrong artefact entirely. These records are the
// right one, and they are unreadable as raw chain output: base64 bodies with
// a multi-kilobyte string inside a JSON field.
func printTranscript(recs []evidence.Record) error {
	found := false
	for _, r := range recs {
		switch r.Kind {
		case "agent-prompt":
			var p struct{ AgentRef, Prompt, Bundle string }
			if err := json.Unmarshal(r.Body, &p); err != nil {
				return err
			}
			found = true
			fmt.Printf("=== PROMPT (agentref %s, %s) ===\n\n%s\n", short(p.AgentRef), r.At.Format(time.RFC3339), p.Prompt)
			fmt.Printf("\n=== BUNDLE ===\n\n%s\n", p.Bundle)
		case "agent-text":
			var tx struct{ AgentRef, StopReason, Text string }
			if err := json.Unmarshal(r.Body, &tx); err != nil {
				return err
			}
			found = true
			fmt.Printf("\n=== AGENT SAID (stop=%s) ===\n\n%s\n", tx.StopReason, tx.Text)
		}
	}
	if !found {
		fmt.Println("no agent transcript on this item — it may predate prompt recording, or no agent ran")
	}
	return nil
}

func short(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

// printChain renders the audit query the way the room and the export do:
// gaps first and loudly, then each span with its verify status, its WORM
// crossing, and the records — the ones naming the sha marked. The CLI and
// the room must not diverge on these facts; a fact one reader has and
// another does not is worse than one nobody has.
func printChain(ch evidence.Chain) {
	if len(ch.Spans) == 0 {
		fmt.Println("nothing recorded about", ch.Subject)
		return
	}
	for _, g := range ch.Gaps {
		fmt.Printf("MISSING %s — %s\n", g.Link, g.Why)
	}
	for _, s := range ch.Spans {
		state := "chain verifies"
		if !s.Whole {
			state = fmt.Sprintf("chain verifies, %d of %d record(s) shown", len(s.Records), s.Total)
		}
		if s.Verify != "" {
			state = "CHAIN BROKEN: " + s.Verify
		}
		fmt.Printf("%s — %d record(s), %s\n", s.Scope, s.Total, state)
		if s.Compacted != nil {
			fmt.Printf("    records %d-%d in WORM at %s\n", s.Compacted.From, s.Compacted.Through, s.Compacted.Export.URI)
		}
		named := map[uint64]bool{}
		for _, seq := range s.Names {
			named[seq] = true
		}
		for _, r := range s.Records {
			mark := " "
			if named[r.Seq] {
				mark = "*"
			}
			who := r.Actor
			if r.OnBehalfOf != "" {
				who += " for " + r.OnBehalfOf
			}
			fmt.Printf("  %s #%-4d %-18s %s\n", mark, r.Seq, r.Kind, who)
		}
	}
	fmt.Println("(*) names this sha; the full artifact is /v1/provenance/<sha>/export")
}

// age renders a duration at the precision an operator cares about: seconds
// while a run is live, days once something has been forgotten.
// printExceptions renders the exception queue in the order a person should
// work it: loudest first, then oldest. The age and the "!" are the point —
// a list of exceptions with no clock on it cannot show the one that has been
// sitting since Friday.
func printExceptions(items []control.Exception) {
	if len(items) == 0 {
		fmt.Println("no exceptions — the factory has nothing waiting on a person")
		return
	}
	for _, it := range items {
		flag := " "
		if it.Overdue {
			flag = "!"
		}
		told := ""
		if it.Escalations > 0 {
			told = fmt.Sprintf(" told=%d", it.Escalations)
		}
		fmt.Printf("%s %-22s %-8s %6s  ingress=%s%s\n    %s\n",
			flag, it.ID, it.Severity.OrDefault(), age(it.AgeSec), it.Ingress, told, it.Note)
	}
	fmt.Printf("\n%d exception(s); ! is past the response-time objective\n", len(items))
}

// printFlakes renders every test the loop has proven flaky, newest
// occurrence first — the count is the point: a test flaked once is the
// gate doing its job, and a test flaked twice or more is why it also shows
// up on the exceptions list.
func printFlakes(items []control.FlakeEntry) {
	if len(items) == 0 {
		fmt.Println("no flakes — nothing has failed then passed on an identical patch")
		return
	}
	for _, it := range items {
		fmt.Printf("x%-3d %-22s last=%s\n    refused=%s passed=%s work_item=%s\n",
			it.Count, it.Subject, it.LastSeen.Format(time.RFC3339),
			short(it.RefusedHead), short(it.PassedHead), it.WorkItem)
	}
	fmt.Printf("\n%d flaky test(s)\n", len(items))
}

func age(sec int64) string {
	switch {
	case sec < 90:
		return fmt.Sprintf("%ds", sec)
	case sec < 5400:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 172800:
		return fmt.Sprintf("%dh", sec/3600)
	}
	return fmt.Sprintf("%dd", sec/86400)
}

// viaDaemon serves the client-mode subcommands over the daemon API — the
// same JSON any UX consumes. Review sampling and sign-off stay direct-store
// for now; they are the operator's own acts and the API grows them when a
// remote control room needs them.
func viaDaemon(addr, token, actor, cmd string, rest []string) error {
	ctx := context.Background()
	c, err := api.Dial(addr, token, actor)
	if err != nil {
		return err
	}
	switch cmd {
	case "status":
		st, err := c.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("tenant=%s box=%s runner=%s stopped=%v pending=%d exceptions=%d up=%ds routes=%v\n",
			st.Tenant, st.Box, st.Runner, st.Stopped, st.Pending, st.Exceptions, st.UptimeSec, st.Routes)
		// The stop with its record: a bare stopped=true sent the operator
		// to the activity feed to find out whether the stop was theirs.
		if st.Claims.Stopped {
			fmt.Printf("stopped by %s at %s: %s\n", st.Claims.Actor, st.Claims.At.Format(time.RFC3339), st.Claims.Reason)
		}
		// Which states that pending number is made of, and the pointer to the
		// verb that lists them. A bare count is the whole complaint.
		if b := st.Backlog; b.Total > 0 {
			fmt.Printf("backlog: %d of %d still owed a run — %s (box queue lists them)\n",
				b.Pending, b.Total, byState(b.ByState))
		}
		// What the window has spent, in both currencies. The room has shown a
		// token total for a while and this printed nothing at all, which is
		// the asymmetry `box queue` already settled once: a fact one reader
		// of the factory has and another does not is worse than one nobody
		// has.
		if sp := st.Spend; sp != nil {
			line := fmt.Sprintf("spend: %d tokens", sp.Tokens)
			if sp.MaxTokens > 0 {
				line += fmt.Sprintf(" of %d", sp.MaxTokens)
			}
			// The parts, because the total cannot be priced without them —
			// a cached read and an output token count the same here and bill
			// an order of magnitude apart.
			if split := tokenSplit(sp); split != "" {
				line += " (" + split + ")"
			}
			if sp.CostMicros > 0 || sp.MaxCostMicros > 0 {
				line += fmt.Sprintf(" · $%.4f", capacity.USD(sp.CostMicros))
				if sp.MaxCostMicros > 0 {
					line += fmt.Sprintf(" of $%.2f", capacity.USD(sp.MaxCostMicros))
				}
			}
			if sp.Unmetered > 0 {
				line += fmt.Sprintf(" · %d run(s) REPORTED NO COST", sp.Unmetered)
			}
			if sp.Unpriced > 0 {
				line += fmt.Sprintf(" · %d run(s) reported no PRICE", sp.Unpriced)
			}
			if sp.Refusing {
				line += " · REFUSING"
			}
			fmt.Println(line)
		}
		if q := st.ExceptionQueue; q.Parked > 0 {
			met := "met"
			if !q.Met {
				met = fmt.Sprintf("MISSED by %d item(s)", q.Overdue)
			}
			fmt.Printf("exception queue: oldest=%s (%s) objective=%s %s\n",
				age(q.OldestAgeSec), q.Oldest, age(q.ObjectiveSec), met)
		}
	case "queue":
		sub := flag.NewFlagSet("queue", flag.ContinueOnError)
		state := sub.String("state", "", "comma-separated states (pending, claimed, waiting, parked, done, failed); empty shows all")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		var states []string
		if *state != "" {
			states = append(states, *state)
		}
		entries, err := c.Items(ctx, states...)
		if err != nil {
			return err
		}
		printQueue(entries)
	case "exceptions":
		items, err := c.Exceptions(ctx)
		if err != nil {
			return err
		}
		printExceptions(items)
	case "flakes":
		items, err := c.Flakes(ctx)
		if err != nil {
			return err
		}
		printFlakes(items)
	case "submit":
		sub := flag.NewFlagSet("submit", flag.ContinueOnError)
		route := sub.String("route", "issues", "configured route name")
		key := sub.String("key", "", "idempotency key for the occurrence")
		task := sub.String("task", "", "what to do")
		summary := sub.String("summary", "", "one-line name for the room; falls back to the task if omitted")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		if *key == "" || *task == "" {
			return fmt.Errorf("submit needs -key and -task")
		}
		res, err := c.OpenIngress(ctx, *route, *key, *task, *summary)
		if err != nil {
			return err
		}
		fmt.Printf("ingress=%s opened=%v item=%s\n", res.Ingress.ID, res.Opened, res.Item)
	case "item":
		if len(rest) == 0 {
			return fmt.Errorf("item needs an id")
		}
		// The session view, not the bare item. `state=claimed head=` was the
		// whole answer for the length of a run, and the CLI and the room must
		// not diverge on it: a fact one reader of the queue has and another
		// does not is worse than one nobody has — the same argument `box
		// queue` settled for the frontier.
		sn, err := c.Session(ctx, rest[0])
		if err != nil {
			return err
		}
		printSession(sn)
	case "provenance":
		if len(rest) == 0 {
			return fmt.Errorf("provenance needs a sha")
		}
		view, err := c.ProvenanceChain(ctx, rest[0])
		if err != nil {
			return err
		}
		printChain(view.Chain)
	case "stop", "resume":
		if len(rest) == 0 {
			return fmt.Errorf("%s needs a reason — it becomes the record", cmd)
		}
		if cmd == "stop" {
			return c.Stop(ctx, rest[0])
		}
		return c.Resume(ctx, rest[0])
	case "transcript":
		if len(rest) == 0 {
			return fmt.Errorf("transcript needs an item id")
		}
		recs, err := c.Evidence(ctx, rest[0])
		if err != nil {
			return err
		}
		return printTranscript(recs)
	case "resolve", "retry":
		sub := flag.NewFlagSet(cmd, flag.ContinueOnError)
		item := sub.String("item", "", "work item id, as `box queue` lists it")
		reason := sub.String("reason", "", "why — it becomes the record")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		if *item == "" {
			return fmt.Errorf("%s needs -item", cmd)
		}
		out, err := c.Reopen(ctx, cmd, *item, *reason)
		if err != nil {
			return err
		}
		fmt.Printf("%s %s state=%s\n%s\n", cmd, out.ID, out.State, out.Note)
		return nil
	default:
		return fmt.Errorf("daemon mode supports status|queue|exceptions|flakes|submit|item|resolve|retry|provenance|stop|resume, got %q", cmd)
	}
	return nil
}

// room fronts the daemon's socket on a loopback port and prints the URL —
// the local browser case, as one command. Loopback with the caller's own
// uid already holding socket access needs no token; anything beyond
// loopback is boxgate's job.
func room(daemonAddr string, rest []string) error {
	sub := flag.NewFlagSet("room", flag.ContinueOnError)
	socket := sub.String("socket", "boxd.sock", "the daemon's unix socket")
	port := sub.Int("port", 0, "loopback port (0 picks one)")
	if err := sub.Parse(rest); err != nil {
		return err
	}
	if daemonAddr != "" {
		if !strings.HasPrefix(daemonAddr, "unix:") {
			return fmt.Errorf("room fronts a local socket; %q is already a network address", daemonAddr)
		}
		*socket = strings.TrimPrefix(daemonAddr, "unix:")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return err
	}
	fmt.Printf("control room: http://%s/  (ctrl-c to close)\n", ln.Addr())
	return http.Serve(ln, gateway.Gate(*socket, ""))
}

// contextCmd manages named daemon endpoints: create, use, ls, rm.
func contextCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: box context <create|use|ls|rm> ...")
	}
	f, err := boxctx.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		sub := flag.NewFlagSet("context create", flag.ContinueOnError)
		daemon := sub.String("daemon", "", "unix:/path or tcp:host:port")
		token := sub.String("token", "", "gate token, stored 0600")
		tenant := sub.String("tenant", "", "default tenant for this context")
		actor := sub.String("actor", "", "default actor")
		if len(args) < 2 {
			return fmt.Errorf("usage: box context create <name> -daemon ... [-token ...] [-tenant ...]")
		}
		name := args[1]
		if err := sub.Parse(args[2:]); err != nil {
			return err
		}
		if *daemon == "" {
			return fmt.Errorf("usage: box context create <name> -daemon ... [-token ...] [-tenant ...]")
		}
		f.Contexts[name] = boxctx.Context{Daemon: *daemon, Token: *token, Tenant: *tenant, Actor: *actor}
		if f.Current == "" {
			f.Current = name
		}
		return boxctx.Save(f)
	case "use":
		if len(args) != 2 {
			return fmt.Errorf("usage: box context use <name>")
		}
		if _, ok := f.Contexts[args[1]]; !ok {
			return fmt.Errorf("no context %q", args[1])
		}
		f.Current = args[1]
		return boxctx.Save(f)
	case "ls":
		for _, n := range boxctx.Names(f) {
			mark := " "
			if n == f.Current {
				mark = "*"
			}
			c := f.Contexts[n]
			fmt.Printf("%s %-16s %-30s tenant=%s\n", mark, n, c.Daemon, c.Tenant)
		}
		return nil
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: box context rm <name>")
		}
		delete(f.Contexts, args[1])
		if f.Current == args[1] {
			f.Current = ""
		}
		return boxctx.Save(f)
	default:
		return fmt.Errorf("unknown context command %q", args[0])
	}
}
