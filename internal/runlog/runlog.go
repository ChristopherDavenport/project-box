// Package runlog keeps what an agent did while it ran: each tool call and what
// it was doing, what the agent said, what policy refused. It is telemetry, not
// evidence. The chain records that a run happened, what it was given, what it
// cost and what it landed; this is the window on the run itself, and it lives
// beside the chain so that it can be pruned on an age without touching a
// record an auditor will ask for. A frame here is never the proof of anything.
package runlog

import (
	"context"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Frame is one thing the agent did, in the order it did it. N is the frame's
// place in its run, assigned on append; At is when it happened, kept as given
// so that the same moment reads the same from the live bus and from here.
type Frame struct {
	Run    string    `json:"run"`
	N      uint64    `json:"n"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Call   string    `json:"call,omitempty"`
	Tool   string    `json:"tool,omitempty"`
	Text   string    `json:"text,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

// Log is the store's side. The tenant is explicit on every call, as on every
// key; a run id alone names nothing.
type Log interface {
	// Append records a frame at the end of its run and returns it with N
	// assigned, and At set to now when it came in zero.
	Append(ctx context.Context, t tenant.ID, f Frame) (Frame, error)
	// Frames is everything kept for a run, oldest first; nil for a run
	// nothing was kept for.
	Frames(ctx context.Context, t tenant.ID, run string) ([]Frame, error)
	// Prune drops every frame older than before, across the tenant's runs,
	// and reports how many. A log that keeps nothing forever is the point of
	// its being telemetry.
	Prune(ctx context.Context, t tenant.ID, before time.Time) (int64, error)
}
