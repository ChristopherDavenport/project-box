package control

import (
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestEveryStateKindHasAStage binds the two registries that read the same
// vocabulary.
//
// stateKinds in the queue says which records carry the item; stageOf here says
// what each record means to an operator watching. They are separate lists over
// the same words, and they drifted the first time anyone extended one: three
// kinds were added for recovery and the exception verbs, stageOf learned none
// of them, and because stage is carried forward from the last record it
// RECOGNISED, the miss did not read as missing. It read as stale.
//
// That is the dangerous shape. A recovered item — pending, claimed by nobody,
// waiting to be picked up — went on reporting "building", which is the one
// thing an operator must never be told, because it names work in progress that
// nobody is doing. A resolved item reported "parked", so a closed exception
// looked open. In both cases the state field was right and the column beside
// it was lying.
func TestEveryStateKindHasAStage(t *testing.T) {
	for _, kind := range queue.StateKinds() {
		if stageOf(kind) == "" {
			t.Errorf("record kind %q folds into the item but has no stage: an item whose last "+
				"transition is %q will show the stage of whatever came before it", kind, kind)
		}
	}
}

// TestStageDoesNotOutliveItsState is the same property from the operator's
// side, on the transition that actually shipped broken: recovery.
func TestStageDoesNotOutliveItsState(t *testing.T) {
	for kind, want := range map[string]string{
		"recovered": "recovered",
		"retried":   "retried",
		"resolved":  "resolved",
	} {
		if got := stageOf(kind); got != want {
			t.Errorf("stageOf(%q) = %q, want %q", kind, got, want)
		}
		if stageOf(kind) == "building" {
			t.Errorf("%q reports as building; nobody is building it", kind)
		}
	}
}
