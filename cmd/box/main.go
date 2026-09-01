// Command box is the control room: the human surface of a factory whose
// execution loop has none. Exceptions, the stop button, review sampling and
// sign-off, and the audit queries — nothing here builds, gates, or lands.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"net"
	"net/http"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/boxctx"
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
	actor := fs.String("actor", os.Getenv("USER"), "operator subject")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 && fs.Arg(0) == "context" {
		return contextCmd(fs.Args()[1:])
	}
	if *tn == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: box -tenant <id> [-db path|-daemon addr] <status|queue|exceptions|submit|item|provenance|stop|resume|room|context> ...")
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
		if cx.Actor != "" && *actor == os.Getenv("USER") {
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
	room := &control.Room{Store: s, Tenant: t, Actor: *actor}
	q := &queue.Queue{Store: s, Runner: *actor}
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
	case "stop":
		if len(rest) == 0 {
			return fmt.Errorf("stop needs a reason — it becomes the record")
		}
		if err := q.Stop(ctx, t, *actor, rest[0]); err != nil {
			return err
		}
		fmt.Println("stopped: no new claims until resume")
	case "resume":
		if len(rest) == 0 {
			return fmt.Errorf("resume needs a reason — it becomes the record")
		}
		if err := q.Resume(ctx, t, *actor, rest[0]); err != nil {
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
		n := 0
		for r, err := range s.Log().BySubject(ctx, t, rest[0]) {
			if err != nil {
				return err
			}
			fmt.Printf("%-16s %-40s seq %-4d actor=%s trace=%s\n", r.Kind, r.Scope, r.Seq, r.Actor, r.Trace)
			n++
		}
		if n == 0 {
			fmt.Println("nothing recorded about", rest[0])
		}
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
// unit, and the note underneath. The note is the answer to "why is it still
// there" — which the queue has always written and a counter can never carry.
func printQueue(entries []control.QueueEntry) {
	if len(entries) == 0 {
		fmt.Println("the queue is empty")
		return
	}
	for _, e := range entries {
		origin := e.Route
		if e.Key != "" {
			origin += "/" + e.Key
		}
		if origin == "" {
			origin = e.Ingress
		}
		fmt.Printf("%-22s %-8s %6s  %-22s %s\n", e.ID, e.State, age(e.AgeSec), origin, e.Task)
		if e.Note != "" {
			fmt.Printf("    %s [%s in state]\n", e.Note, age(e.InStateSec))
		}
	}
	fmt.Printf("\n%d item(s)\n", len(entries))
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
	case "submit":
		sub := flag.NewFlagSet("submit", flag.ContinueOnError)
		route := sub.String("route", "issues", "configured route name")
		key := sub.String("key", "", "idempotency key for the occurrence")
		task := sub.String("task", "", "what to do")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		if *key == "" || *task == "" {
			return fmt.Errorf("submit needs -key and -task")
		}
		res, err := c.OpenIngress(ctx, *route, *key, *task)
		if err != nil {
			return err
		}
		fmt.Printf("ingress=%s opened=%v item=%s\n", res.Ingress.ID, res.Opened, res.Item)
	case "item":
		if len(rest) == 0 {
			return fmt.Errorf("item needs an id")
		}
		it, err := c.Item(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Printf("%s state=%s head=%s\n%s\n", it.ID, it.State, it.Head, it.Note)
	case "provenance":
		if len(rest) == 0 {
			return fmt.Errorf("provenance needs a sha")
		}
		recs, err := c.Provenance(ctx, rest[0])
		if err != nil {
			return err
		}
		for _, r := range recs {
			fmt.Printf("%-16s %-40s seq %-4d actor=%s\n", r.Kind, r.Scope, r.Seq, r.Actor)
		}
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
		return fmt.Errorf("daemon mode supports status|queue|exceptions|submit|item|resolve|retry|provenance|stop|resume, got %q", cmd)
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
