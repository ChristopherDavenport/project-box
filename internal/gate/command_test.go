package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

func TestVerdictKeepsHeadFailureLinesAheadOfTheTail(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("assert user count equals 3\n")
	b.WriteString("--- FAIL: TestUserCount (0.00s)\n")
	for i := 0; i < 100; i++ {
		b.WriteString("ok  	example.com/pkg" + strconv.Itoa(i) + "	0.001s\n")
	}
	transcript := b.String()

	c := Command{Label: "test", Dir: filepath.Join(dir, "ws")}
	v, err := c.verdict(forge.DecisionFail, `"go test ./..." exited non-zero`, []byte(transcript))
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if !strings.Contains(v.Reason, "--- FAIL: TestUserCount") {
		t.Errorf("Reason missing head failure line: %q", v.Reason)
	}
	if !strings.Contains(v.Reason, "assert user count equals 3") {
		t.Errorf("Reason missing the line preceding the failure: %q", v.Reason)
	}
}

func TestVerdictOnPassKeepsTheReasonShapeUnchanged(t *testing.T) {
	dir := t.TempDir()
	transcript := "$ go build ./...\n$ go test ./...\nok  	example.com/pkg	0.001s\n"

	c := Command{Label: "build", Dir: filepath.Join(dir, "ws")}
	v, err := c.verdict(forge.DecisionPass, "all commands passed", []byte(transcript))
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if v.Decision != forge.DecisionPass {
		t.Errorf("Decision = %v, want pass", v.Decision)
	}
	want := "all commands passed\n" + transcript
	if v.Reason != want {
		t.Errorf("Reason = %q, want %q", v.Reason, want)
	}
}

func TestVerdictWritesTheFullTranscriptToASiblingFile(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	transcript := "$ go test ./...\n" + strings.Repeat("ok pkg\n", 500) + "--- FAIL: TestX\npanic: boom\n"

	c := Command{Label: "test", Dir: ws}
	v, err := c.verdict(forge.DecisionFail, "exited non-zero", []byte(transcript))
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}

	if len(v.Evidence) != 1 {
		t.Fatalf("Evidence = %v, want exactly one ref", v.Evidence)
	}
	ref := v.Evidence[0]
	if ref.URI == "inline" {
		t.Fatalf("URI is still the literal %q", ref.URI)
	}
	if ref.URI == ws || strings.HasPrefix(filepath.Base(ref.URI), filepath.Base(ws)+string(filepath.Separator)) {
		t.Errorf("URI %q is not a sibling of workspace dir %q", ref.URI, ws)
	}
	if filepath.Dir(ref.URI) != filepath.Dir(ws) {
		t.Errorf("URI %q is not in the same parent directory as workspace dir %q", ref.URI, ws)
	}

	got, err := os.ReadFile(ref.URI)
	if err != nil {
		t.Fatalf("reading evidence file: %v", err)
	}
	if string(got) != transcript {
		t.Errorf("evidence file contents = %q, want the full transcript %q", got, transcript)
	}

	sum := sha256.Sum256([]byte(transcript))
	wantDigest := hex.EncodeToString(sum[:])
	if ref.Digest != wantDigest {
		t.Errorf("Digest = %q, want %q", ref.Digest, wantDigest)
	}
}

func TestVerdictBoundsTheNumberOfHeadFailureBlocksKept(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < maxHeadFailureBlocks*3; i++ {
		b.WriteString("assertion " + strconv.Itoa(i) + " failed\n")
		b.WriteString("--- FAIL: TestRepeat" + strconv.Itoa(i) + "\n")
	}
	// Pad well past the 2048-byte tail so none of the FAIL lines above also
	// land in the tail, keeping this a clean count of the head-scan cap.
	b.WriteString(strings.Repeat("ok  \texample.com/pkg\t0.001s\n", 200))
	transcript := b.String()

	c := Command{Label: "test", Dir: filepath.Join(dir, "ws")}
	v, err := c.verdict(forge.DecisionFail, "exited non-zero", []byte(transcript))
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}

	got := strings.Count(v.Reason, "--- FAIL: TestRepeat")
	if got != maxHeadFailureBlocks {
		t.Errorf("kept %d failure blocks, want exactly the cap of %d", got, maxHeadFailureBlocks)
	}
}

func TestEvaluateFailReportsHeadFailureLinesFromEarlyInALongTranscript(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	var script strings.Builder
	script.WriteString("echo 'assert widget count equals 1'\n")
	script.WriteString("echo '--- FAIL: TestWidgetCount (0.00s)'\n")
	for i := 0; i < 100; i++ {
		script.WriteString("echo 'ok  \texample.com/pkg" + strconv.Itoa(i) + "\t0.001s'\n")
	}
	script.WriteString("exit 1\n")

	c := Command{
		Label:    "test",
		Dir:      ws,
		Commands: [][]string{{"sh", "-c", script.String()}},
	}

	v, err := c.Evaluate(context.Background(), forge.ChangeSet{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Decision != forge.DecisionFail {
		t.Fatalf("Decision = %v, want fail", v.Decision)
	}
	if !strings.Contains(v.Reason, "--- FAIL: TestWidgetCount") {
		t.Errorf("Reason missing head failure line: %q", v.Reason)
	}
	if !strings.Contains(v.Reason, "assert widget count equals 1") {
		t.Errorf("Reason missing the line preceding the failure: %q", v.Reason)
	}
	if v.Evidence[0].URI == "inline" {
		t.Errorf("URI is still the literal %q", v.Evidence[0].URI)
	}
	if _, err := os.Stat(v.Evidence[0].URI); err != nil {
		t.Errorf("evidence file not written: %v", err)
	}
}
