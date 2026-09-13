package control

import (
	"strings"
	"sync"
	"time"
)

// DefaultKeep bounds the frames the live bus holds across every run at once.
//
// Stated rather than implied, because the alternative is a slow leak in a
// daemon designed to run overnight unattended. One measured session made 110
// tool calls in 21 minutes, so this is a handful of concurrent runs' worth of
// window — enough that an operator opening the room mid-run sees how the run
// got where it is, and far short of a session's whole history.
const DefaultKeep = 2000

// Frame is one thing a running agent did, as it did it.
//
// Nothing here is the record of anything. The chain is: which tools ran and
// which were denied arrive on it as a tally when the run settles, the agent's
// own account arrives as agent-text, and the box verbs arrive as their own
// records. This is those same facts while they are still news, which is a
// property telemetry has and evidence deliberately does not — see
// ARCHITECTURE, "Telemetry is not evidence".
type Frame struct {
	// Seq orders the whole bus and is the cursor a subscriber holds. Global
	// rather than per-item, because a subscriber watching every unit at once
	// is the fan-out case and it needs one cursor, not N.
	Seq  uint64    `json:"seq"`
	Item string    `json:"item"`
	At   time.Time `json:"at"`
	// Kind is [acp.UpdateKind]'s string, carried as one rather than imported:
	// the room renders what agents do and must not depend on the runtime that
	// drives them.
	Kind string `json:"kind"`
	// Call is the tool call's id on the wire, so that a later frame about the
	// same call — the title, once the adapter knows it — can be read as the
	// same call and not as a second one.
	Call   string `json:"call,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Text   string `json:"text,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Session is the agentref of the ACP session that produced this frame,
	// the same fact [runlog.Frame] carries — the live bus and the kept log
	// tell the same story.
	Session string `json:"session,omitempty"`
}

// Live is the room's window into runs that have not settled — the ACP stream,
// fanned in from every agent the daemon is running and fanned back out to
// whoever is watching.
//
// It is deliberately in memory and deliberately bounded, and both follow from
// what it is for. A claimed item is a black box for the length of a run, and
// closing that is a live-view problem, not a retention problem: a frame is
// interesting for as long as the run is going and is superseded by the chain
// the moment it settles. Putting the per-call sequence on the evidence chain
// instead would contradict the reason it is on span events today, and would
// buy an audit answer nobody asked for at the price of a hash-chained,
// WORM-exported record per tool call.
//
// What that costs is honest and small: a daemon restart loses the window, and
// so does the run it was watching, because the agent is that daemon's child.
type Live struct {
	// Keep bounds the ring; zero means [DefaultKeep].
	Keep int
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time

	mu     sync.Mutex
	seq    uint64
	frames []Frame
}

func (l *Live) now() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

func (l *Live) keep() int {
	if l.Keep <= 0 {
		return DefaultKeep
	}
	return l.Keep
}

// Append records one frame against a work item and returns it with its
// sequence assigned. Safe on a nil receiver so that a builder wired without a
// bus needs no branch of its own.
func (l *Live) Append(item string, f Frame) Frame {
	if l == nil {
		return Frame{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	f.Seq, f.Item = l.seq, item
	if f.At.IsZero() {
		f.At = l.now()
	}
	l.frames = append(l.frames, f)
	if over := len(l.frames) - l.keep(); over > 0 {
		// Copy down rather than reslice: a reslice keeps the whole backing
		// array alive, which for a window whose entire job is to be bounded
		// would mean it never actually is.
		l.frames = append(l.frames[:0], l.frames[over:]...)
	}
	return f
}

// Since returns every frame after a cursor, oldest first. A subscriber holds
// the last Seq it saw; zero asks for the whole window.
//
// The dropped-frames case is deliberately silent here and loud at the reader:
// a cursor older than the ring gets what survives, and [Live.Head] is how a
// caller learns the window moved past it.
func (l *Live) Since(seq uint64) []Frame {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Frame
	for _, f := range l.frames {
		if f.Seq > seq {
			out = append(out, f)
		}
	}
	return out
}

// Item returns one unit's frames, oldest first — the timeline behind one card.
func (l *Live) Item(id string) []Frame {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Frame
	for _, f := range l.frames {
		if f.Item == id {
			out = append(out, f)
		}
	}
	return out
}

// Watching reports that this bus holds the run — that the daemon answering is
// the one running the agent, and an empty timeline therefore means the agent
// has done nothing rather than that nobody is looking.
//
// The distinction is the point. "Twenty minutes claimed and silent" is a run
// to go and look at; "twenty minutes claimed and we cannot see it" is a
// different daemon's work, or a window that rolled over. A reader left to
// infer this from an empty list would infer the alarming one.
func (l *Live) Watching(id string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, f := range l.frames {
		if f.Item == id {
			return true
		}
	}
	return false
}

// Prefixed returns the distinct item ids in the window whose id has the given
// prefix, in first-seen order.
//
// Item and Watching both need an id in hand already; a judgement's card does
// not have one to hold until this exists, because nothing indexes the window
// by anything but the id a caller already knows.
func (l *Live) Prefixed(prefix string) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, f := range l.frames {
		if !strings.HasPrefix(f.Item, prefix) || seen[f.Item] {
			continue
		}
		seen[f.Item] = true
		out = append(out, f.Item)
	}
	return out
}

// Head is the newest sequence issued, so a subscriber can start from now
// rather than replaying the window.
func (l *Live) Head() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}
