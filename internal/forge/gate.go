package forge

import "context"

// Gate decides whether a [ChangeSet] may land.
//
// A gate evaluates the ChangeSet whole. Checking each repository in isolation
// answers the wrong question: a contract change that is fine on its own and a
// client regeneration that is fine on its own can still be wrong together, and
// that pairing is precisely what a multi-repo control plane exists to make
// routine.
type Gate interface {
	// Name identifies the gate in a [Verdict] and in the audit trail.
	Name() string
	// Evaluate reports whether the ChangeSet satisfies this gate. A gate that
	// cannot reach a conclusion *yet* returns [DecisionPending]; one that
	// cannot reach a conclusion *at all* returns an error, so that
	// infrastructure trouble is never recorded as a rejected change.
	//
	// Evaluate is called repeatedly while a gate is pending and must be
	// idempotent: asking a human gate twice must not request two approvals.
	Evaluate(ctx context.Context, cs ChangeSet) (Verdict, error)
}

// DiffJudge is an optional [Gate] capability: a gate that judges the content
// of the change rather than the state of the base it was built on. A gate
// that satisfies this and passed a commit is still satisfied by any other
// commit carrying the identical patch, which is what lets a rebase stage
// carry that gate's verdict forward instead of re-running it.
//
// Declared here rather than as a required Gate method so that most gates —
// a check that runs against the base's tree, a human approval that binds to
// the exact commit a person looked at — simply do not implement it, and the
// loop finds that out with a type assertion rather than importing every
// gate's package to ask.
type DiffJudge interface {
	// JudgesDiff reports true for a gate whose verdict depends only on the
	// diff's content, not on the base commit it was evaluated against.
	JudgesDiff() bool
}

// Decision is a gate's answer. Three states, not two: a human gate cannot
// decide synchronously, and collapsing "not yet" into "no" would fail changes
// that are merely waiting.
type Decision string

const (
	DecisionPass Decision = "pass"
	DecisionFail Decision = "fail"
	// DecisionPending means the gate cannot answer yet and the ChangeSet is
	// waiting on something outside this process — a reviewer, a soak, an
	// external check. A pending ChangeSet is *waiting*, which is distinct from
	// *parked*: parking is for work that went wrong, waiting is for work that
	// is proceeding normally and has not finished.
	DecisionPending Decision = "pending"
)

// Verdict is one gate's answer for one ChangeSet.
type Verdict struct {
	Gate     string
	Decision Decision
	// Reason is the human-readable justification, shown to the agent or
	// operator who has to act on a rejection.
	Reason      string
	Evidence    []EvidenceRef
	Resolutions []Resolution
}

// Resolution is one earlier finding's fate under the diff this Verdict
// judges, named by which attempt and which gate found it. Data beside the
// reason, never a second verdict: Decision alone still governs pass or
// fail, so a fail whose Resolutions mark every earlier finding resolved
// still fails, and an earlier refusal is information a gate was told, never
// a verdict carried forward.
type Resolution struct {
	Attempt  int
	Gate     string
	Resolved bool
	Note     string
}

// EvidenceKindRecord marks an [EvidenceRef] whose URI is an [evidence.Record]
// ID rather than a log path or artifact digest — the kind a reader outside
// this process (git.PRLander's GitHub check) can follow straight back into
// the evidence chain. Shared between internal/loop, which attaches it, and
// internal/forge/git, which reads it back: the two cannot share a helper
// function directly (internal/loop imports internal/forge/git, so the
// reverse would cycle), so the string is the seam instead.
const EvidenceKindRecord = "evidence-record"

// EvidenceRef is a durable pointer to what a gate actually observed: a test
// log, a build record, a review, a policy decision. Verdicts are recorded in
// the evidence log and outlive the run that produced them, so evidence is
// referenced by location rather than inlined.
type EvidenceRef struct {
	Kind string
	// URI locates the evidence. Anything a later reader can dereference: a log
	// object, an artifact digest, an evidence record id.
	URI    string
	Digest string
}

// Resolve reduces a set of verdicts to one decision.
//
// A failure anywhere fails the whole ChangeSet, and outranks a pending verdict:
// there is no point waiting on a reviewer for a change that has already failed
// its tests. An empty slice fails rather than passes — a ChangeSet no gate
// examined has not been reviewed, and silence must never read as approval.
func Resolve(vs []Verdict) Decision {
	if len(vs) == 0 {
		return DecisionFail
	}
	pending := false
	for _, v := range vs {
		switch v.Decision {
		case DecisionFail:
			return DecisionFail
		case DecisionPending:
			pending = true
		}
	}
	if pending {
		return DecisionPending
	}
	return DecisionPass
}
