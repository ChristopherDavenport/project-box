// Command box is the control room: the human surface of a factory whose
// execution loop has none. Exceptions, the stop button, review sampling and
// sign-off, and the audit queries — nothing here builds, gates, or lands.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

func main() {
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
	if *tn == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: box -tenant <id> [-db path] <exceptions|stop|resume|sample|signoff|provenance> ...")
	}
	t := tenant.ID(*tn)
	if err := t.Validate(); err != nil {
		return err
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
	case "exceptions":
		items, err := room.Exceptions(ctx)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("no exceptions — the factory has nothing waiting on a person")
			return nil
		}
		for _, it := range items {
			fmt.Printf("%-24s %-8s ingress=%s\n    %s\n", it.ID, it.State, it.Ingress, it.Note)
		}
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
	case "exceptions":
		items, err := c.Exceptions(ctx)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("no exceptions — the factory has nothing waiting on a person")
			return nil
		}
		for _, it := range items {
			fmt.Printf("%-24s %-8s ingress=%s\n    %s\n", it.ID, it.State, it.Ingress, it.Note)
		}
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
	default:
		return fmt.Errorf("daemon mode supports status|exceptions|submit|item|provenance|stop|resume, got %q", cmd)
	}
	return nil
}
