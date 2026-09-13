package boxcfg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
	"github.com/ChristopherDavenport/project-box/internal/loop"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "box.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAgentArgvAcceptsBothForms: the common case stays one word, and a
// confining wrapper is expressible (issue #6). A bare string is ONE argument,
// never a shell line — a config that reinterprets spaces runs something the
// operator did not write.
func TestAgentArgvAcceptsBothForms(t *testing.T) {
	for _, tc := range []struct {
		name, agent string
		want        []string
	}{
		{"scalar", `agent: claude-agent-acp`, []string{"claude-agent-acp"}},
		{"scalar with spaces stays one argument", `agent: "my agent"`, []string{"my agent"}},
		{"argv", `agent: [docker, run, --rm, -w, "{{workspace}}", img, claude-agent-acp]`,
			[]string{"docker", "run", "--rm", "-w", "{{workspace}}", "img", "claude-agent-acp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    seat: mason
    `+tc.agent+`
    repos:
      - name: app
        remote: /tmp/app.git
`))
			if err != nil {
				t.Fatal(err)
			}
			if got := []string(c.Boxes[0].Agent); !slices.Equal(got, tc.want) {
				t.Fatalf("agent argv = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConfineFoldsIntoTheAgentArgv: the daemon runs one argv and knows
// nothing about confinement, so declaring an image has to produce the wrapper
// rather than sit beside it. The rendering itself is asserted in
// internal/agent/confine; what matters here is that it reaches Agent and that
// the agent command survives at the tail.
func TestConfineFoldsIntoTheAgentArgv(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    confine:
      image: box/agent:1
      user: "1000:1000"
      env: [ANTHROPIC_API_KEY]
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	argv := []string(c.Boxes[0].Agent)
	if len(argv) < 2 || argv[0] != "docker" || argv[len(argv)-1] != "claude-agent-acp" {
		t.Fatalf("agent argv = %q, want the agent wrapped in a container", argv)
	}
	if !slices.ContainsFunc(argv, func(a string) bool { return strings.Contains(a, confine.ContextPlaceholder) }) {
		t.Errorf("agent argv = %q, want the context set mounted too — a wrapper told only about the workspace confines the agent out of its own reading material", argv)
	}
}

// TestConfineIsRefusedWhenItWouldConfineNothing: a repo's build command runs
// on the daemon itself, so a confine block with no agent is a security
// posture that silently applies to nothing — and an image is the one field
// with no sensible default, because the absence of one is a wrapper that
// cannot start.
func TestConfineIsRefusedWhenItWouldConfineNothing(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"no agent to wrap", `
tenant: acme
boxes:
  - name: shop
    confine:
      image: box/agent:1
    repos:
      - name: app
        remote: /tmp/app.git
        build: go build ./...
`},
		{"no image to wrap it in", `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    confine:
      user: "1000:1000"
    repos:
      - name: app
        remote: /tmp/app.git
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, tc.body)); err == nil {
				t.Fatal("accepted a confine block that confines nothing")
			}
		})
	}
}

// TestNoAgentAndNoBuildIsRefused: the existing opinion, still held once the
// agent field became a list — an empty list is no agent, not an agent named
// "".
func TestNoAgentAndNoBuildIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err == nil {
		t.Fatal("a repo with neither agent nor build command was accepted")
	}
}

// TestTokenBudgetLoads: the brake is configurable in the currency the invoice
// is denominated in.
func TestTokenBudgetLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
budget:
  max_concurrent: 3
  max_tokens_per_window: 2000000
  window: 1h
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Budget.MaxTokensPerWindow != 2_000_000 {
		t.Fatalf("token budget = %d", c.Budget.MaxTokensPerWindow)
	}
	if c.WindowDuration().Hours() != 1 {
		t.Fatalf("window = %v", c.WindowDuration())
	}
}

// TestCredentialRefusesUnknownSpellings: budget.credential offers exactly
// plan or key, absent included; anything else is refused naming what it
// offers.
func TestCredentialRefusesUnknownSpellings(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
budget:
  credential: subscription
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err == nil || !strings.Contains(err.Error(), "plan") || !strings.Contains(err.Error(), "key") {
		t.Fatalf("budget.credential: subscription: err = %v, want a refusal naming plan and key", err)
	}
}

// TestABrakeWithNoCredentialIsRefused: window_reserve or max_cost_per_window
// binds an account, and an operator who set the brake without naming which
// account it is should be told at load — the same call the config already
// makes about half a retention policy — rather than discover it the day a
// key in the daemon's environment moves every run onto an invoice the box
// thought was a subscription's.
func TestABrakeWithNoCredentialIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, brake string
	}{
		{"window_reserve", "window_reserve: 0.5"},
		{"max_cost_per_window", "max_cost_per_window: 25.00"},
	} {
		_, err := Load(writeCfg(t, `
tenant: acme
budget:
  `+tc.brake+`
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
		if err == nil || !strings.Contains(err.Error(), "credential") {
			t.Errorf("%s with no credential: err = %v, want a refusal naming credential", tc.name, err)
		}
	}
}

// TestACredentialWithNoBrakeLoads: an unbraked box that never asked to
// budget is not refused for staying silent about which account it runs on —
// only a brake makes the question load-bearing.
func TestACredentialWithNoBrakeLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
budget:
  max_concurrent: 2
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatalf("an unbraked box with no credential named was refused: %v", err)
	}
	if c.Budget.Credential != "" {
		t.Fatalf("credential = %q, want absent", c.Budget.Credential)
	}
}

// TestModelAndSessionLoad: the box names the model by the value the agent
// lists, and the rest of the session by option id.
func TestModelAndSessionLoad(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    model: sonnet
    session:
      effort: medium
      fast_mode: "true"
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	bx := c.Boxes[0]
	if bx.Model != "sonnet" || bx.Session["effort"] != "medium" || bx.Session["fast_mode"] != "true" {
		t.Fatalf("model=%q session=%v", bx.Model, bx.Session)
	}
}

// TestSessionRefusesTheSpellingsThatHaveFields. Two ways to say the model
// would be two places for the record to disagree with itself; the permission
// mode is the tool policy's and travels through session/set_mode, never as an
// option. Both are refused at load with the field they wanted.
func TestSessionRefusesTheSpellingsThatHaveFields(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"model", "model has its own field"},
		{"mode", "permission mode"},
	} {
		_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    session:
      `+tc.key+`: x
    repos:
      - name: app
        remote: /tmp/app.git
`))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("session.%s: err = %v, want %q", tc.key, err, tc.want)
		}
	}
	// And a session with no agent has nothing to configure.
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    model: sonnet
    repos:
      - name: app
        remote: /tmp/app.git
        build: make
`))
	if err == nil || !strings.Contains(err.Error(), "no agent") {
		t.Errorf("model without an agent: err = %v", err)
	}
}

// TestForgeRepoCarriesTheRepositoryCheck: a unit that names no section still
// needs a check to iterate on, and forge.Repo is where every consumer past
// boxcfg reads it from — configured or defaulted, it must survive the
// crossing (issue #46).
func TestForgeRepoCarriesTheRepositoryCheck(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        check: ./hack/check.sh
      - name: lib
        remote: /tmp/lib.git
`))
	if err != nil {
		t.Fatal(err)
	}
	repos := c.Boxes[0].Repos
	if got := repos[0].ForgeRepo().Check; got != "./hack/check.sh" {
		t.Fatalf("configured check: ForgeRepo().Check = %q, want %q", got, "./hack/check.sh")
	}
	if got := repos[1].ForgeRepo().Check; got != "true" {
		t.Fatalf("defaulted check: ForgeRepo().Check = %q, want %q", got, "true")
	}
}

// TestLandPullRequestNeedsAToken: a repo asking to land through a pull
// request without a declared token source would leave the daemon unable to
// open one when the loop first tries to land — refused at Load rather than
// discovered mid-run (issue #22).
func TestLandPullRequestNeedsAToken(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
`))
	if err == nil || !strings.Contains(err.Error(), "land_token") {
		t.Fatalf("pull_request with no land_token: err = %v", err)
	}
}

// TestLandUnknownValueIsRefused: "land" only knows one spelling for opening a
// pull request first; anything else is a typo the operator should hear about
// at load, not a silently-ignored default back to a direct push.
func TestLandUnknownValueIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: merge_queue
`))
	if err == nil || !strings.Contains(err.Error(), `land "merge_queue"`) {
		t.Fatalf("unknown land value: err = %v", err)
	}
}

// TestLandPullRequestWithTokenLoads: the happy path — a repo that declares
// both the pull_request mode and its token source loads cleanly, and the
// values survive to where the daemon reads them to wire git.PRLander.
func TestLandPullRequestWithTokenLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
        land_token: gh
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Boxes[0].Repos[0]
	if r.Land != LandPullRequest || r.LandToken != "gh" {
		t.Fatalf("repo land = %q land_token = %q, want %q and %q", r.Land, r.LandToken, LandPullRequest, "gh")
	}
}

// TestLandDefaultsToDirectPush: a repo that says nothing about landing keeps
// today's behavior — Land empty, no token required.
func TestLandDefaultsToDirectPush(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].Land; got != "" {
		t.Fatalf("repo land = %q, want empty", got)
	}
}

// TestReviewDefaultsToCommandGateAlone: a repo that says nothing about review
// keeps today's behavior — no reviewer in the gate set.
func TestReviewDefaultsToCommandGateAlone(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].Review; got != "" {
		t.Fatalf("repo review = %q, want empty", got)
	}
}

// TestReviewUnknownValueIsRefused: "review" only knows one spelling for
// putting a judgement in front of a landing; anything else is a typo the
// operator should hear about at load.
func TestReviewUnknownValueIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        review: careful
`))
	if err == nil || !strings.Contains(err.Error(), `review "careful"`) {
		t.Fatalf("unknown review value: err = %v", err)
	}
}

// TestReviewJudgementNeedsAnAgent: a build command cannot judge a diff, so a
// box with no agent asking for review: judgement is refused at Load rather
// than failing every change it admits.
func TestReviewJudgementNeedsAnAgent(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: go build ./...
        review: judgement
`))
	if err == nil || !strings.Contains(err.Error(), "review: judgement with no agent") {
		t.Fatalf("review: judgement with no agent: err = %v", err)
	}
}

// TestReviewJudgementRefusesTheOptimisticLane: the optimistic lane lands
// first and gates afterward, so a reviewer there would read a diff already
// on the branch — not a review before the push, which is the whole point.
func TestReviewJudgementRefusesTheOptimisticLane(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        lane: optimistic
        review: judgement
`))
	if err == nil || !strings.Contains(err.Error(), "review: judgement with lane: optimistic") {
		t.Fatalf("review: judgement with lane: optimistic: err = %v", err)
	}
}

// TestReviewJudgementLoads: the happy path — a repo declaring review:
// judgement on the (default) gated lane, with an agent, loads cleanly.
func TestReviewJudgementLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        lane: gated
        review: judgement
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].Review; got != ReviewJudgement {
		t.Fatalf("repo review = %q, want %q", got, ReviewJudgement)
	}
}

// TestSelfDeployAbsentByDefault: no self_deploy: block is the every-existing-
// config case, and it must stay exactly today's behavior — no chore, ever.
func TestSelfDeployAbsentByDefault(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
        check: 'go test ./...'
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Boxes[0].SelfDeploy != nil {
		t.Fatalf("self_deploy = %+v, want nil (absent means never)", c.Boxes[0].SelfDeploy)
	}
}

// TestSelfDeployNeedsFrom: the branch to follow is not optional.
func TestSelfDeployNeedsFrom(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {repo: app}
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
        check: 'go test ./...'
`))
	if err == nil || !strings.Contains(err.Error(), "self_deploy needs from") {
		t.Fatalf("self_deploy with no from: err = %v", err)
	}
}

// TestSelfDeployDefaultsRepoWhenThereIsOnlyOne: naming the repo is optional
// when there is nothing to disambiguate.
func TestSelfDeployDefaultsRepoWhenThereIsOnlyOne(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main}
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
        check: 'go test ./...'
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].SelfDeploy.Repo; got != "app" {
		t.Fatalf("self_deploy.repo = %q, want the box's sole repo %q", got, "app")
	}
}

// TestSelfDeployRefusesAmbiguityAcrossSeveralRepos: picking one implicitly
// would be exactly the derived value this system refuses everywhere else.
func TestSelfDeployRefusesAmbiguityAcrossSeveralRepos(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main}
    repos:
      - name: api
        remote: /tmp/api.git
        build: 'true'
        check: 'go test ./...'
      - name: web
        remote: /tmp/web.git
        build: 'true'
        check: 'go test ./...'
`))
	if err == nil || !strings.Contains(err.Error(), "self_deploy has no repo") {
		t.Fatalf("self_deploy with two repos and no repo named: err = %v", err)
	}
}

// TestSelfDeployRefusesAnUnknownRepo: a self_deploy.repo that names nothing
// in the box is a config the operator should be told about at load.
func TestSelfDeployRefusesAnUnknownRepo(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main, repo: nope}
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
        check: 'go test ./...'
`))
	if err == nil || !strings.Contains(err.Error(), `self_deploy.repo "nope" is not one of this box's repositories`) {
		t.Fatalf("self_deploy.repo naming an unknown repo: err = %v", err)
	}
}

// TestSelfDeployRefusesARepoWithNoCheck: a self-deploy target with no check
// cannot be judged, and a commit that cannot be judged must not be deployed —
// this must be caught even though an absent check: elsewhere defaults to
// "true" and would otherwise look like a check that always passes.
func TestSelfDeployRefusesARepoWithNoCheck(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main}
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
`))
	if err == nil || !strings.Contains(err.Error(), "self_deploy.repo app has no check") {
		t.Fatalf("self_deploy.repo with no check: err = %v", err)
	}
}

// TestSelfDeployLoadsWithAnExplicitRepo: the happy path with several repos
// and the ambiguity resolved by naming one.
func TestSelfDeployLoadsWithAnExplicitRepo(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main, repo: api}
    repos:
      - name: api
        remote: /tmp/api.git
        build: 'true'
        check: 'go test ./...'
      - name: web
        remote: /tmp/web.git
        build: 'true'
        check: 'go test ./...'
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].SelfDeploy; got == nil || got.From != "main" || got.Repo != "api" {
		t.Fatalf("self_deploy = %+v, want {From: main, Repo: api}", got)
	}
}

// TestSelfDeployRefusesAnUndeclaredSection: a self_deploy.sections entry that
// names no section of the target repo is a config the operator should be
// told about at load, the same way an unknown repo is.
func TestSelfDeployRefusesAnUndeclaredSection(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main, sections: [nope]}
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
        check: 'go test ./...'
        sections:
          - name: daemon
            purpose: the daemon
            paths: [internal/daemon]
`))
	if err == nil || !strings.Contains(err.Error(), `self_deploy.sections names "nope", which is not a declared section of repo app`) {
		t.Fatalf("self_deploy.sections naming an undeclared section: err = %v", err)
	}
}

// TestSelfDeployLoadsWithDeclaredSections: naming sections that are all
// declared on the target repo is the happy path.
func TestSelfDeployLoadsWithDeclaredSections(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    self_deploy: {from: main, sections: [daemon]}
    repos:
      - name: app
        remote: /tmp/app.git
        build: 'true'
        check: 'go test ./...'
        sections:
          - name: daemon
            purpose: the daemon
            paths: [internal/daemon]
          - name: room
            purpose: the room
            paths: [web]
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].SelfDeploy.Sections; len(got) != 1 || got[0] != "daemon" {
		t.Fatalf("self_deploy.sections = %v, want [daemon]", got)
	}
}

// TestApprovalAbsentByDefault: a repo that says nothing about approval keeps
// today's behavior — no human gate, machine-gated.
func TestApprovalAbsentByDefault(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].Approval.Count; got != 0 {
		t.Fatalf("approval.count = %d, want 0 (machine-gated)", got)
	}
}

// TestApprovalNegativeCountIsRefused: a negative count is not a valid
// requirement — refused at Load rather than compared against forever.
func TestApprovalNegativeCountIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
        land_token: gh
        lane: gated
        approval: {count: -1}
`))
	if err == nil || !strings.Contains(err.Error(), "approval.count -1") {
		t.Fatalf("negative approval.count: err = %v", err)
	}
}

// TestApprovalNeedsPullRequestLanding: a count with no pull_request landing
// has nowhere for a GitHub review to be, named explicitly in issue #66.
func TestApprovalNeedsPullRequestLanding(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        lane: gated
        approval: {count: 1}
`))
	if err == nil || !strings.Contains(err.Error(), "approval.count with land") {
		t.Fatalf("approval.count with no pull_request landing: err = %v", err)
	}
}

// TestApprovalRefusesTheOptimisticLane: the optimistic lane lands first and
// gates afterward, so an approver there would be asked about a change
// already on the branch — the same reasoning as review: judgement's own
// optimistic-lane refusal, applied here as a deliberate addition beyond what
// issue #66 names explicitly. optimistic is Lane's default, so a repo that
// names a count and says nothing about lane hits this refusal too.
func TestApprovalRefusesTheOptimisticLane(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
        land_token: gh
        approval: {count: 1}
`))
	if err == nil || !strings.Contains(err.Error(), "approval.count with lane: optimistic") {
		t.Fatalf("approval.count with default (optimistic) lane: err = %v", err)
	}
}

// TestApprovalRefusesTheOptimisticLaneExplicit: same refusal, lane named
// explicitly rather than left to default.
func TestApprovalRefusesTheOptimisticLaneExplicit(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
        land_token: gh
        lane: optimistic
        approval: {count: 1}
`))
	if err == nil || !strings.Contains(err.Error(), "approval.count with lane: optimistic") {
		t.Fatalf("approval.count with lane: optimistic: err = %v", err)
	}
}

// TestApprovalRefusesUnknownSource: only "github" is known today — nothing
// in this tree records a direct attestation as an approval.Approval, so
// accepting "direct" would wait forever on an approval no interface can
// give.
func TestApprovalRefusesUnknownSource(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
        land_token: gh
        lane: gated
        approval: {count: 1, sources: [direct]}
`))
	if err == nil || !strings.Contains(err.Error(), `approval.sources "direct"`) {
		t.Fatalf("approval.sources: direct: err = %v", err)
	}
}

// TestApprovalLoads: the happy path — a repo declaring a count on the gated
// lane with pull_request landing loads cleanly, and the values survive to
// where the daemon reads them.
func TestApprovalLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        land: pull_request
        land_token: gh
        lane: gated
        approval: {count: 1}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Boxes[0].Repos[0]
	if r.Approval.Count != 1 {
		t.Fatalf("approval.count = %d, want 1", r.Approval.Count)
	}
	if r.Land != LandPullRequest || r.LandToken != "gh" || r.Lane != "gated" {
		t.Fatalf("repo = %+v, want land=pull_request land_token=gh lane=gated", r)
	}
}

// TestUnsetRootIsBesideNotInsideTheWorkingDirectory: an unset root must not
// resolve to a path under the daemon's cwd, or a builder can infer the
// operator's checkout from its own working directory and write to it
// (issue #73).
func TestUnsetRootIsBesideNotInsideTheWorkingDirectory(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsLocal(c.Root) {
		t.Fatalf("root = %q resolves inside the working directory, want beside it", c.Root)
	}
}

// TestUnsetDBAndListenFollowRoot: db and listen ride the same knob as root
// when they too are unset, so the daemon's whole state footprint moves
// together (issue #73).
func TestUnsetDBAndListenFollowRoot(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(c.Root, "box.db"); c.DB != want {
		t.Fatalf("db = %q, want %q", c.DB, want)
	}
	if want := "unix:" + filepath.Join(c.Root, "boxd.sock"); c.Listen != want {
		t.Fatalf("listen = %q, want %q", c.Listen, want)
	}
}

// TestHousekeepEveryDefaultsTo5m: absent means the housekeeping chores
// (mirror prune, escalation sweep, compaction, run-log prune, self-deploy)
// still run — on a 5-minute clock, distinct from the tick interval Every
// carries (issue #152).
func TestHousekeepEveryDefaultsTo5m(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.HousekeepEvery != "5m" {
		t.Fatalf("housekeep_every = %q, want 5m", c.HousekeepEvery)
	}
	if got := c.HousekeepEveryDuration(); got != 5*time.Minute {
		t.Fatalf("HousekeepEveryDuration() = %v, want 5m", got)
	}
}

// TestHousekeepEveryParses: an explicit value is honored, and is not
// confused with the tick interval Every.
func TestHousekeepEveryParses(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
every: 1s
housekeep_every: 90s
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.HousekeepEveryDuration(); got != 90*time.Second {
		t.Fatalf("HousekeepEveryDuration() = %v, want 90s", got)
	}
	if got := c.EveryDuration(); got != time.Second {
		t.Fatalf("EveryDuration() = %v, want 1s — every and housekeep_every must not collide", got)
	}
}

// TestHousekeepEveryRefusesAMalformedDuration: the same refusal style as
// every, drain, and retention.keep_for.
func TestHousekeepEveryRefusesAMalformedDuration(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
housekeep_every: not-a-duration
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err == nil || !strings.Contains(err.Error(), "housekeep_every") {
		t.Fatalf("housekeep_every malformed: err = %v, want it to name housekeep_every", err)
	}
}

// TestWidenAbsentDefaultsToTests: issue #182's own words call widen:tests a
// reasonable default, so a box naming no widen: policy at all must resolve to
// [loop.WidenTests] — auto-widen a test-only cross-section need — rather than
// parking every such escalation for a person, which is what an empty string
// would mean if it survived Load unresolved.
func TestWidenAbsentDefaultsToTests(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Widen; got != loop.WidenTests {
		t.Fatalf("widen = %q, want %q (the default)", got, loop.WidenTests)
	}
}

// TestWidenNoneParses: a box that wants a person on every cross-section
// escalation writes this explicitly, and Load must carry it through
// unchanged rather than defaulting past it.
func TestWidenNoneParses(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    widen: none
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Widen; got != loop.WidenNone {
		t.Fatalf("widen = %q, want %q", got, loop.WidenNone)
	}
}

// TestWidenRefusesAMisspelling: only "tests" and "none" are known, and a
// misspelling — including the truthy-looking "true" a YAML author reaches
// for on instinct — is refused at Load naming the accepted spellings, the
// same style as TestSessionRefusesTheSpellingsThatHaveFields.
func TestWidenRefusesAMisspelling(t *testing.T) {
	for _, bad := range []string{"test", "true"} {
		_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    widen: `+bad+`
    repos:
      - name: app
        remote: /tmp/app.git
`))
		if err == nil || !strings.Contains(err.Error(), "tests") || !strings.Contains(err.Error(), "none") {
			t.Fatalf("widen: %s: err = %v, want it to name the accepted spellings tests/none", bad, err)
		}
	}
}

// TestActorClaimsAbsentByDefault: an unset allowlist is not a lax one — it
// parses to no entries at all, which the api package reads as "no claim is
// ever honored" (see internal/api/api.go's mustActor).
func TestActorClaimsAbsentByDefault(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ActorClaims) != 0 {
		t.Fatalf("actor_claims = %v, want none absent from the file", c.ActorClaims)
	}
}

// TestActorClaimsLoads: the ordinary case, an operator naming who may be
// claimed as an on-behalf-of over the API.
func TestActorClaimsLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
actor_claims: [chris, ci-bot]
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"chris", "ci-bot"}; len(c.ActorClaims) != len(want) || c.ActorClaims[0] != want[0] || c.ActorClaims[1] != want[1] {
		t.Fatalf("actor_claims = %v, want %v", c.ActorClaims, want)
	}
}

// TestActorClaimsRefusesAnEmptyEntry: a name-shaped list with a hole in it is
// a config an operator almost certainly did not mean, and unknown must not
// read as zero — silently admitting an empty claim would be exactly that.
func TestActorClaimsRefusesAnEmptyEntry(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
actor_claims: [chris, ""]
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err == nil || !strings.Contains(err.Error(), "actor_claims") {
		t.Fatalf("err = %v, want it to name actor_claims", err)
	}
}

// TestActorClaimsRefusesADuplicate: two entries naming the same claim are
// certainly a mistake, not an intensified grant — the allowlist is a set,
// and Load should say so rather than silently deduplicating.
func TestActorClaimsRefusesADuplicate(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
actor_claims: [chris, chris]
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err == nil || !strings.Contains(err.Error(), "chris") {
		t.Fatalf("err = %v, want it to name the duplicated claim", err)
	}
}
