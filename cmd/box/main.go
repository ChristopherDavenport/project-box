// Command box is the control room: the human surface of a factory whose
// execution loop has none. Exceptions, the stop button, review sampling and
// sign-off, and the audit queries — nothing here builds, gates, or lands.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

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
	db := fs.String("db", "box.db", "store path")
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
