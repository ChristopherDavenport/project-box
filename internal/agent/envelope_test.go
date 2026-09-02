package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The worked example throughout: an estate whose operator has named two
// classes. A published CVE with a known upgrade path is `public` work; an
// embargoed finding is `embargoed`, and both arrive as "fix a security issue"
// and route to the same builder.
const (
	public    Class = "public"
	embargoed Class = "embargoed"
)

// ceiling is the Box's bound — what the Box handles at all. It names no class
// of its own, so the same Box carries work at both.
func ceiling() *Envelope {
	return &Envelope{
		Endpoints: []string{"vendor-shared", "vendor-inregion", "kb"},
		Context:   []Class{public, embargoed},
		Redaction: RedactSecrets,
		Payload:   PayloadText,
	}
}

func classified(c Class) *Envelope {
	return &Envelope{
		Class:     c,
		Endpoints: []string{"vendor-shared", "vendor-inregion", "kb"},
		Context:   []Class{public, embargoed},
		Redaction: RedactSecrets,
		Payload:   PayloadText,
	}
}

func classifiedLayers(model string) StaticLayers {
	return StaticLayers{{
		Name: "org", Scope: ScopeBase,
		Source:       Source{Kind: SourceGit, Digest: "d-base", Repo: "policies", Commit: "abc"},
		Instructions: "Follow the org conventions.",
		Model:        model,
	}}
}

func classifiedRequest(e *Envelope) Request {
	return Request{Tenant: "acme", Role: RoleBuilder, Box: "shop", Repo: "app", Run: "run-1", Envelope: e}
}

// TestUnclassifiedWorkIsRefusedNotDefaulted.
//
// Unclassified is the state every mis-scoped request arrives in, so it is the
// state that has to be safe. The rule is PLAN §5's twice over — a principal
// with an empty grant set can do nothing, an unset rollout means one — applied
// to data handling: absence means minimum, and the minimum here runs nothing.
func TestUnclassifiedWorkIsRefusedNotDefaulted(t *testing.T) {
	ctx := context.Background()
	c := &Composer{Source: classifiedLayers("vendor-shared"), Ceiling: ceiling()}

	for name, req := range map[string]Request{
		"no envelope at all": classifiedRequest(nil),
		"an envelope naming no class": classifiedRequest(&Envelope{
			// Everything permissive except the one field that says what this
			// work is. A mis-scoped request looks exactly like this.
			Endpoints: []string{"vendor-shared"},
			Context:   []Class{public, embargoed},
			Payload:   PayloadText,
		}),
	} {
		b, err := c.Resolve(ctx, req)
		if !errors.Is(err, ErrEnvelope) {
			t.Fatalf("%s: err = %v, want a refusal", name, err)
		}
		if b.Ref != "" {
			t.Fatalf("%s: refused work still resolved a bundle (%s)", name, b.Ref)
		}
	}

	// And the other direction, so the refusal is the classification rather
	// than the composer being broken: the same request WITH a class resolves.
	if _, err := c.Resolve(ctx, classifiedRequest(classified(embargoed))); err != nil {
		t.Fatalf("declared work was refused too: %v", err)
	}
}

// TestNoCeilingIsNoRegime. A Box that has declared no ceiling is every Box
// today, and the resolver must impose nothing on it — a classification layer
// that silently switched itself on would refuse the entire factory.
func TestNoCeilingIsNoRegime(t *testing.T) {
	b, err := (&Composer{Source: classifiedLayers("")}).Resolve(context.Background(), classifiedRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	if b.Envelope != nil {
		t.Fatalf("an unclassified box invented an envelope: %v", b.Envelope)
	}
}

// TestAnAgentLayerCannotWidenItsEnvelope.
//
// The intersection is the property that makes the envelope worth declaring
// rather than documenting. An agent layer is the natural place to try: it is
// where the model, the MCP servers, and the context set are stated, and a
// layer that could name a wider endpoint would make the declaration advisory.
func TestAnAgentLayerCannotWidenItsEnvelope(t *testing.T) {
	ctx := context.Background()
	// The ceiling permits one endpoint. The declaration asks for three.
	narrow := &Envelope{Endpoints: []string{"vendor-inregion"}, Context: []Class{embargoed}, Payload: PayloadText}
	c := &Composer{Source: classifiedLayers("vendor-inregion"), Ceiling: narrow}

	b, err := c.Resolve(ctx, classifiedRequest(classified(embargoed)))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Envelope.Endpoints; len(got) != 1 || got[0] != "vendor-inregion" {
		t.Fatalf("endpoints = %v, want the intersection [vendor-inregion]", got)
	}
	if got := b.Envelope.Context; len(got) != 1 || got[0] != embargoed {
		t.Fatalf("context = %v, want the intersection [embargoed]", got)
	}

	// A layer reaching for one of the two endpoints the declaration named and
	// the ceiling did not is refused, not quietly served the narrow one.
	widen := classifiedRequest(classified(embargoed))
	widen.Extra = []Layer{{
		Name: "sneaky", Scope: ScopeTask, Source: Source{Kind: SourceInline, Digest: "d"},
		Model: "vendor-shared",
	}}
	if _, err := c.Resolve(ctx, widen); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("a layer widened the envelope: err = %v", err)
	}

	// A stricter posture on either side wins, in both directions: the
	// declaration cannot loosen the ceiling's redaction, and the ceiling
	// cannot loosen the declaration's.
	loose := classified(embargoed)
	loose.Redaction = RedactNone
	got, err := loose.Narrow(Envelope{Redaction: RedactAll, Endpoints: loose.Endpoints, Context: loose.Context, Payload: PayloadText})
	if err != nil {
		t.Fatal(err)
	}
	if got.Redaction != RedactAll {
		t.Fatalf("redaction = %q, want the stricter posture", got.Redaction)
	}

	// And a class the ceiling does not carry is refused rather than bent.
	pinned := &Composer{Source: classifiedLayers("vendor-inregion"), Ceiling: classified(public)}
	if _, err := pinned.Resolve(ctx, classifiedRequest(classified(embargoed))); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("a declaration outside its ceiling's class resolved: %v", err)
	}
}

// TestARestrictedRunCannotResolveANonPermittedEndpoint.
//
// Refusal, not silent narrowing, and the unnamed model counts: "whatever the
// runtime was configured with" is not an answer to which endpoint saw the
// data.
func TestARestrictedRunCannotResolveANonPermittedEndpoint(t *testing.T) {
	ctx := context.Background()
	inregion := &Envelope{Class: embargoed, Endpoints: []string{"vendor-inregion"}, Context: []Class{embargoed}, Payload: PayloadText}

	for name, layers := range map[string]StaticLayers{
		"a model outside the permitted set": classifiedLayers("vendor-shared"),
		"no model at all":                   classifiedLayers(""),
	} {
		c := &Composer{Source: layers}
		if _, err := c.Resolve(ctx, classifiedRequest(inregion)); !errors.Is(err, ErrEnvelope) {
			t.Fatalf("%s: err = %v, want a refusal", name, err)
		}
	}

	// An MCP server is an endpoint by the envelope's definition — it is a
	// place the payload goes — so naming one outside the set is the same
	// refusal, which is the case a model-only check would miss.
	withMCP := classifiedLayers("vendor-inregion")
	withMCP[0].MCP = []MCPRef{{Name: "public-search", URL: "https://example.invalid"}}
	if _, err := (&Composer{Source: withMCP}).Resolve(ctx, classifiedRequest(inregion)); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("an MCP server outside the envelope resolved: %v", err)
	}

	// The permitted endpoint resolves, so the refusals above are the envelope
	// and not the check refusing everything.
	if _, err := (&Composer{Source: classifiedLayers("vendor-inregion")}).Resolve(ctx, classifiedRequest(inregion)); err != nil {
		t.Fatalf("the permitted endpoint was refused: %v", err)
	}
}

// TestContextAboveTheRunsClassIsRefused.
//
// The context set is what an agent is GIVEN, which is the half the envelope
// governs; confinement is what stops it taking more. Neither substitutes for
// the other, and this is the first half: a run classified for public work
// cannot be handed the embargoed repository.
func TestContextAboveTheRunsClassIsRefused(t *testing.T) {
	ctx := context.Background()
	publicOnly := &Envelope{Class: public, Endpoints: []string{"vendor-shared"}, Context: []Class{public}, Payload: PayloadText}

	layers := classifiedLayers("vendor-shared")
	layers[0].Context = ContextSet{Repos: []ContextRepo{{Repo: "advisories", Commit: "c1", Class: embargoed}}}
	c := &Composer{Source: layers}

	_, err := c.Resolve(ctx, classifiedRequest(publicOnly))
	if !errors.Is(err, ErrEnvelope) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "advisories") {
		t.Fatalf("the refusal does not name the material: %v", err)
	}

	// Unclassified material is not a free pass either: an envelope reads what
	// it says it reads, and material nobody classified is material nobody
	// vouched for.
	layers[0].Context = ContextSet{Repos: []ContextRepo{{Repo: "scratch", Commit: "c1"}}}
	if _, err := (&Composer{Source: layers}).Resolve(ctx, classifiedRequest(publicOnly)); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("unclassified context material was readable: %v", err)
	}

	// Its own class reads.
	layers[0].Context = ContextSet{Repos: []ContextRepo{{Repo: "docs", Commit: "c1", Class: public}}}
	if _, err := (&Composer{Source: layers}).Resolve(ctx, classifiedRequest(publicOnly)); err != nil {
		t.Fatalf("material at the run's own class was refused: %v", err)
	}
}

// TestReferenceOnlyPayloadRefusesTheIngressText.
//
// The sharpest field of the five. The strongest handling of an embargoed
// finding is not redacting its description — it is opening work that says
// "this test fails, make it pass" and never telling the agent it is a security
// issue. A composer cannot invent that substitute, so it refuses rather than
// sending the text it was handed.
func TestReferenceOnlyPayloadRefusesTheIngressText(t *testing.T) {
	ctx := context.Background()
	byRef := &Envelope{Class: embargoed, Endpoints: []string{"vendor-inregion"}, Context: []Class{embargoed}, Payload: PayloadReference}
	c := &Composer{Source: classifiedLayers("vendor-inregion")}

	req := classifiedRequest(byRef)
	req.Task = "CVE-2026-0001 is embargoed until Tuesday; the parser overruns on a crafted header."
	if _, err := c.Resolve(ctx, req); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("the ingress text reached the prompt under a reference-only envelope: %v", err)
	}

	// The substitute the operator does supply is an ordinary layer, and it
	// resolves.
	req.Task = ""
	req.Extra = []Layer{{
		Name: "substitute", Scope: ScopeTask, Source: Source{Kind: SourceInline, Digest: "d"},
		Instructions: "TestParseHeader fails. Make it pass.",
	}}
	b, err := c.Resolve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.Instructions, "embargoed") {
		t.Fatalf("the payload leaked into the prompt:\n%s", b.Instructions)
	}

	// An unstated posture is reference-only, not text: absence means minimum
	// on this field as on every other.
	silent := &Envelope{Class: embargoed, Endpoints: []string{"vendor-inregion"}, Context: []Class{embargoed}}
	loud := classifiedRequest(silent)
	loud.Task = "anything at all"
	if _, err := c.Resolve(ctx, loud); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("an unstated payload posture defaulted to permissive: %v", err)
	}
}

// TestTheEnvelopeIsPartOfTheAgentref.
//
// The bundle is content-addressed, so folding the envelope into the digest
// gets the record almost for free: a different envelope is a different agent,
// exactly as continuity notes already are. Without this, two runs over
// identical layers under different data handling would be indistinguishable on
// the chain — which is the question a data-handling review asks.
func TestTheEnvelopeIsPartOfTheAgentref(t *testing.T) {
	ctx := context.Background()
	c := &Composer{Source: classifiedLayers("vendor-inregion")}

	wide := &Envelope{Class: embargoed, Endpoints: []string{"vendor-inregion", "kb"}, Context: []Class{public, embargoed}, Payload: PayloadText}
	narrow := &Envelope{Class: embargoed, Endpoints: []string{"vendor-inregion"}, Context: []Class{embargoed}, Payload: PayloadText}

	a, err := c.Resolve(ctx, classifiedRequest(wide))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Resolve(ctx, classifiedRequest(narrow))
	if err != nil {
		t.Fatal(err)
	}
	if a.Ref == b.Ref {
		t.Fatal("two runs under different envelopes share an agentref")
	}
	// Still deterministic: the same envelope re-resolves to the same ref.
	again, err := c.Resolve(ctx, classifiedRequest(narrow))
	if err != nil {
		t.Fatal(err)
	}
	if again.Ref != b.Ref {
		t.Fatalf("the same envelope resolved twice to %q and %q", b.Ref, again.Ref)
	}
	// A run under no envelope at all is a third agent, not either of them.
	none, err := (&Composer{Source: classifiedLayers("vendor-inregion")}).Resolve(ctx, classifiedRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	if none.Ref == a.Ref || none.Ref == b.Ref {
		t.Fatal("an unclassified run shares an agentref with a classified one")
	}

	// And it renders, because a digest can settle a dispute and cannot answer
	// a question.
	rendered, err := c.Explain(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "envelope: class=embargoed endpoints=[vendor-inregion]") {
		t.Fatalf("Explain does not render the envelope in force:\n%s", rendered)
	}
}

// TestTheClassOfContextMaterialIsPartOfTheAgentref. Reclassifying a repository
// changes what a run was permitted to see, which is a behavioural change and
// must move the digest like every other one.
func TestTheClassOfContextMaterialIsPartOfTheAgentref(t *testing.T) {
	ctx := context.Background()
	mk := func(class Class) string {
		layers := classifiedLayers("")
		layers[0].Context = ContextSet{Repos: []ContextRepo{{Repo: "advisories", Commit: "c1", Class: class}}}
		b, err := (&Composer{Source: layers}).Resolve(ctx, classifiedRequest(nil))
		if err != nil {
			t.Fatal(err)
		}
		return b.Ref
	}
	if mk(public) == mk(embargoed) {
		t.Fatal("reclassifying context material did not change the agentref")
	}
}
