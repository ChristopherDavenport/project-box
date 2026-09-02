package agent

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrEnvelope marks every refusal the data envelope raises, so a caller can
// tell "this work may not be run under this handling" from an ordinary
// resolution failure without matching on prose.
var ErrEnvelope = errors.New("agent: data envelope")

// Class is a data class — the sensitivity of an origination's payload and of
// the work it opens.
//
// The taxonomy is the operator's. Nothing here ships a set of class names or
// an order over them: an embargoed vulnerability finding is material
// non-public information in one estate and an ordinary ticket in another, and
// a mechanism that guessed would be wrong in exactly the estates that care.
// What this package supplies is the envelope, its intersection, and the two
// places it refuses.
type Class string

// Unclassified is the absence of a declared class. It is not a class an
// operator may use for work: an envelope that names it is undeclared, and
// undeclared work is refused rather than defaulted, because unclassified is
// the state every mis-scoped request arrives in and therefore the state that
// has to be safe. Same rule as an empty grant set and an unset rollout.
const Unclassified Class = ""

// Redaction is what may appear in spans, evidence, and transcripts for work
// under this envelope.
type Redaction string

const (
	// RedactAll keeps anything derived from the payload out of telemetry and
	// out of narrative evidence entirely.
	RedactAll Redaction = "all"
	// RedactSecrets is the collector's standing posture: credentials masked,
	// everything else exported.
	RedactSecrets Redaction = "secrets"
	// RedactNone exports what the pipeline produces.
	RedactNone Redaction = "none"
)

// redactions orders the postures strictest first.
var redactions = []Redaction{RedactAll, RedactSecrets, RedactNone}

// Payload is whether the origination's own text may be sent to an endpoint.
type Payload string

const (
	// PayloadReference withholds the ingress text. Only a reference to it may
	// travel, which is the sharpest handling available: the strongest
	// treatment of an embargoed finding is not redacting its description but
	// opening work that says "this test fails, make it pass" and never
	// telling the agent it is a security issue. Whether that is over-clever
	// is a per-class judgement, which is why it is a declared option rather
	// than a decision someone makes in prose at dispatch time.
	PayloadReference Payload = "reference"
	// PayloadText permits the origination's own words.
	PayloadText Payload = "text"
)

// payloads orders the postures strictest first.
var payloads = []Payload{PayloadReference, PayloadText}

// Envelope declares what data class an origination's work may touch and which
// endpoints may see it.
//
// It is an input to bundle resolution that can only narrow it. Delegation
// intersects and never unions here as everywhere else, applied to data
// handling: an envelope intersects with the ceiling in force, and the resolved
// bundle's endpoints and readable context are the intersection. A restricted
// origination cannot be widened by an agent layer, which is the property that
// makes the envelope worth declaring rather than documenting.
//
// The zero value is undeclared, and every field's zero is the restrictive end:
// no endpoints permitted, no context readable, redact everything, send only a
// reference. Absence means minimum.
type Envelope struct {
	// Class is the data class of the payload and of the work it opens.
	Class Class
	// Endpoints names every place the work's data may be sent — a model
	// provider, deployment or region, and equally an MCP server, since an
	// endpoint here is defined by where the payload goes rather than by what
	// answers. Empty permits none.
	Endpoints []string
	// Context is the classes of material readable under this envelope. A set
	// rather than a threshold: a threshold needs an order over the taxonomy,
	// and the taxonomy is the operator's, so an order would be a second
	// declaration this mechanism has not been given. Empty reads nothing.
	Context []Class
	// Redaction and Payload are declarations that ride the bundle digest and
	// the evidence record. Payload is enforced at resolve; Redaction's
	// enforcement point is the telemetry and evidence path, which does not
	// consult this yet.
	Redaction Redaction
	Payload   Payload
}

// Declared reports whether this envelope says anything. An envelope naming no
// class is the undeclared state, whatever else it carries.
func (e Envelope) Declared() bool { return e.Class != Unclassified }

// Narrow applies a ceiling to a declared envelope and returns what is actually
// in force.
//
// The ceiling is the bound an origination's declaration is held to — a Box's,
// today; a Box's and its kind declaration's once there is one. A ceiling
// naming no class bounds the handling without pinning the class, which is what
// lets one Box carry work at several classes; a ceiling naming one pins the
// Box to it, and a declaration at a different class is refused rather than
// bent to fit. Refusal, not silent narrowing: a narrowed run nobody was told
// about is worse than a refused one.
func (e Envelope) Narrow(ceiling Envelope) (Envelope, error) {
	if !e.Declared() {
		return Envelope{}, fmt.Errorf("%w: undeclared work cannot be narrowed; it is refused", ErrEnvelope)
	}
	if ceiling.Declared() && ceiling.Class != e.Class {
		return Envelope{}, fmt.Errorf("%w: class %q is outside the ceiling's class %q", ErrEnvelope, e.Class, ceiling.Class)
	}
	return Envelope{
		Class:     e.Class,
		Endpoints: intersectStrings(e.Endpoints, ceiling.Endpoints),
		Context:   intersectClasses(e.Context, ceiling.Context),
		// Strictest wins, which is the LOWER rank: both orders run strictest
		// first so that the zero value — an unstated posture on either side —
		// is the strict end rather than the loose one.
		Redaction: redactions[min(rankOf(redactions, e.Redaction), rankOf(redactions, ceiling.Redaction))],
		Payload:   payloads[min(rankOf(payloads, e.Payload), rankOf(payloads, ceiling.Payload))],
	}, nil
}

// PermitsEndpoint reports whether the work's data may be sent somewhere.
func (e Envelope) PermitsEndpoint(name string) bool {
	return name != "" && slices.Contains(e.Endpoints, name)
}

// MayRead reports whether material of a class may be read under this envelope.
func (e Envelope) MayRead(c Class) bool { return slices.Contains(e.Context, c) }

// SendsPayload reports whether the origination's own text may travel. An
// unstated posture does not: absence means minimum on this field as on the
// others.
func (e Envelope) SendsPayload() bool {
	return rankOf(payloads, e.Payload) > rankOf(payloads, PayloadReference)
}

// Check refuses a composed bundle that reaches past the envelope.
//
// This is the resolve-time enforcement point, and it refuses rather than
// quietly dropping the offending endpoint or context repo: a bundle that
// resolved to less than it named is one whose operator believes something
// false about the run.
func (e Envelope) Check(b Bundle) error {
	if !e.Declared() {
		return fmt.Errorf("%w: no class is declared for this work", ErrEnvelope)
	}
	// An unnamed model is the runtime's default, which is an endpoint nobody
	// wrote down. Under classification that is a refusal and not an
	// abstention — the whole question the envelope answers is which endpoint
	// saw the data, and "whatever was configured" is not an answer.
	if !e.PermitsEndpoint(b.Model) {
		if b.Model == "" {
			return fmt.Errorf("%w: class %q names endpoints %v and the bundle names no model; an endpoint that is not named cannot be checked",
				ErrEnvelope, e.Class, e.Endpoints)
		}
		return fmt.Errorf("%w: class %q may not reach endpoint %q (permitted: %v)", ErrEnvelope, e.Class, b.Model, e.Endpoints)
	}
	for _, m := range b.MCP {
		if !e.PermitsEndpoint(m.Name) {
			return fmt.Errorf("%w: class %q may not reach MCP server %q (permitted: %v)", ErrEnvelope, e.Class, m.Name, e.Endpoints)
		}
	}
	for _, r := range b.Context.Repos {
		if !e.MayRead(r.Class) {
			return fmt.Errorf("%w: class %q may not read context repository %q, classified %q (readable: %v)",
				ErrEnvelope, e.Class, r.Repo, r.Class, e.Context)
		}
	}
	return nil
}

// String renders the envelope for Explain and for the evidence record. One
// line, deterministic, and it names every field: "the envelope in force" is
// the answer to a data-handling review's only question, so it has to be
// readable rather than reconstructable.
func (e Envelope) String() string {
	class := string(e.Class)
	if class == "" {
		class = "(unclassified)"
	}
	ctx := make([]string, len(e.Context))
	for i, c := range e.Context {
		ctx[i] = string(c)
	}
	return fmt.Sprintf("class=%s endpoints=[%s] context=[%s] redaction=%s payload=%s",
		class, strings.Join(e.Endpoints, " "), strings.Join(ctx, " "),
		redactions[rankOf(redactions, e.Redaction)], payloads[rankOf(payloads, e.Payload)])
}

// rankOf orders a posture, strictest first. An unstated one — and an unknown
// one, which is the same failure a class higher up the stack has — ranks as
// the strictest rather than the loosest.
func rankOf[T ~string](order []T, v T) int {
	if i := slices.Index(order, v); i >= 0 {
		return i
	}
	return 0
}

func intersectStrings(a, b []string) []string {
	var out []string
	for _, s := range a {
		if slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

func intersectClasses(a, b []Class) []Class {
	var out []Class
	for _, c := range a {
		if slices.Contains(b, c) {
			out = append(out, c)
		}
	}
	return out
}
