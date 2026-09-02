package agent

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// resolveDomain versions the bundle digest. Any change to merge semantics or
// the canonical serialization MUST change it: two bundles that merged
// differently must never share a ref.
//
// v2 folds in the data envelope and the class of each context repository. A
// run permitted to see one class and reach one endpoint is not the same agent
// as the same layers under a wider envelope, and the digest is where that has
// to be true — which is also what makes "this run was permitted to see class X
// and ran against endpoint Y" answerable from the record with no new
// machinery.
const resolveDomain = "project-box/agentref/v2"

// LayerSource supplies the governed layers for a request, most general first:
// base, role, box, repo. The task layer and the request's Extra layers are the
// resolver's to append — they are per-request by nature and come from the
// caller, not from governance.
type LayerSource interface {
	Layers(ctx context.Context, req Request) ([]Layer, error)
}

// StaticLayers is an in-memory LayerSource for tests and for wiring before
// definitions live in git.
type StaticLayers []Layer

// Layers returns the layers as given.
func (s StaticLayers) Layers(context.Context, Request) ([]Layer, error) { return s, nil }

// restrictiveness orders permission modes, most restrictive first. When
// layers disagree, the most restrictive stated mode wins; a layer must be
// able to narrow what a broader one allowed, never widen it.
var restrictiveness = []string{"plan", "dontAsk", "default", "acceptEdits", "bypassPermissions"}

func moreRestrictive(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if slices.Index(restrictiveness, a) <= slices.Index(restrictiveness, b) {
		return a
	}
	return b
}

// roleDefaultMode is the mode used when no layer states one: the most
// restrictive mode the role can do its work under, never the most permissive.
// A gate only judges, so it plans; the writing roles auto-approve edits and
// still ask for everything else. The harness default out there is
// bypassPermissions — an omitted policy must never resolve to that.
var roleDefaultMode = map[Role]string{
	RoleBuilder:  "acceptEdits",
	RoleGate:     "plan",
	RoleDeployer: "acceptEdits",
	RoleCurator:  "acceptEdits",
}

// Composer is the reference [Resolver].
type Composer struct {
	Source LayerSource
	// Ceiling bounds the data handling of everything this composer resolves.
	// Nil is no classification regime at all, which is where a Box starts and
	// what every Box is today; set, it makes classification mandatory —
	// a request declaring no envelope is refused rather than defaulted, and
	// every declaration is intersected with this before it is enforced.
	Ceiling *Envelope
}

// Resolve composes the bundle for a request. It is deterministic: the same
// request against the same layers produces the same Ref, which is the whole
// point — the ref, not the layer names, is what evidence records.
//
// Merge rules, per field:
//
//   - Instructions concatenate in layer order.
//   - Skills, plugins and MCP servers merge by name; a more specific layer's
//     entry replaces a broader one's. Plugins key on [PluginRef.ID], so the
//     same plugin from two marketplaces is two entries rather than one
//     silently winning.
//   - Context repos merge by repo name the same way; knowledge scopes union.
//   - Tool denies union: a deny anywhere holds everywhere. Tool allows
//     intersect across the layers that state one: a narrower layer can only
//     narrow. An empty allow list is no opinion, not "allow nothing".
//   - PermissionMode: the most restrictive stated mode wins; with none
//     stated, the role's restrictive default applies.
//   - Model: the most specific non-empty value wins — a preference, not a
//     safety property, so later layers may change it freely.
//
// The data envelope is not a merge rule, because it is not a layer field. It
// is a bound the merged result is checked against: see [Envelope], and
// [Composer.envelope] for how the origination's declaration and the ceiling
// combine.
func (c *Composer) Resolve(ctx context.Context, req Request) (Bundle, error) {
	layers, err := c.Source.Layers(ctx, req)
	if err != nil {
		return Bundle{}, fmt.Errorf("agent: loading layers: %w", err)
	}
	env, err := c.envelope(req)
	if err != nil {
		return Bundle{}, err
	}
	layers = append(slices.Clone(layers), req.Extra...)
	if req.Task != "" {
		// The origination's own words are the payload. Under a
		// reference-only posture they may not travel at all, and refusing is
		// the only honest answer here: the substitute — a failing test, a
		// path, a ticket number — is a judgement about the work that a
		// composer cannot make from the text it was handed.
		if env != nil && !env.SendsPayload() {
			return Bundle{}, fmt.Errorf("%w: class %q sends a reference, not its payload, and this request carries the ingress text as its task",
				ErrEnvelope, env.Class)
		}
		layers = append(layers, Layer{
			Name: "task", Scope: ScopeTask,
			Source:       Source{Kind: SourceInline, Digest: hashText(req.Task)},
			Instructions: req.Task,
		})
	}
	if len(layers) == 0 {
		return Bundle{}, fmt.Errorf("agent: no layers resolve for role %q in box %q", req.Role, req.Box)
	}

	b := Bundle{Tenant: req.Tenant, Role: req.Role, Box: req.Box, Layers: layers, Envelope: env}
	var prose []string
	skills := map[string]SkillRef{}
	var skillOrder []string
	plugins := map[string]PluginRef{}
	var pluginOrder []string
	mcps := map[string]MCPRef{}
	var mcpOrder []string
	repos := map[string]ContextRepo{}
	var repoOrder []string
	knowledge := map[string]bool{}
	var knowledgeOrder []string
	denies := map[string]bool{}
	var denyOrder []string
	var allowSets [][]string

	for _, l := range layers {
		if l.Instructions != "" {
			prose = append(prose, l.Instructions)
		}
		for _, sk := range l.Skills {
			if _, seen := skills[sk.Name]; !seen {
				skillOrder = append(skillOrder, sk.Name)
			}
			skills[sk.Name] = sk
		}
		for _, p := range l.Plugins {
			if _, seen := plugins[p.ID()]; !seen {
				pluginOrder = append(pluginOrder, p.ID())
			}
			plugins[p.ID()] = p
		}
		for _, m := range l.MCP {
			if _, seen := mcps[m.Name]; !seen {
				mcpOrder = append(mcpOrder, m.Name)
			}
			mcps[m.Name] = m
		}
		for _, r := range l.Context.Repos {
			if _, seen := repos[r.Repo]; !seen {
				repoOrder = append(repoOrder, r.Repo)
			}
			repos[r.Repo] = r
		}
		for _, k := range l.Context.Knowledge {
			if !knowledge[k] {
				knowledge[k] = true
				knowledgeOrder = append(knowledgeOrder, k)
			}
		}
		for _, d := range l.Tools.Deny {
			if !denies[d] {
				denies[d] = true
				denyOrder = append(denyOrder, d)
			}
		}
		if len(l.Tools.Allow) > 0 {
			allowSets = append(allowSets, l.Tools.Allow)
		}
		b.Tools.PermissionMode = moreRestrictive(b.Tools.PermissionMode, l.Tools.PermissionMode)
		if l.Model != "" {
			b.Model = l.Model
		}
	}

	b.Instructions = strings.Join(prose, "\n\n")
	for _, n := range skillOrder {
		b.Skills = append(b.Skills, skills[n])
	}
	for _, n := range pluginOrder {
		b.Plugins = append(b.Plugins, plugins[n])
	}
	for _, n := range mcpOrder {
		b.MCP = append(b.MCP, mcps[n])
	}
	for _, n := range repoOrder {
		b.Context.Repos = append(b.Context.Repos, repos[n])
	}
	b.Context.Knowledge = knowledgeOrder
	b.Tools.Deny = denyOrder
	if len(allowSets) > 0 {
		inter := allowSets[0]
		for _, set := range allowSets[1:] {
			var next []string
			for _, a := range inter {
				if slices.Contains(set, a) {
					next = append(next, a)
				}
			}
			inter = next
		}
		b.Tools.Allow = inter
	}
	if b.Tools.PermissionMode == "" {
		b.Tools.PermissionMode = roleDefaultMode[req.Role]
		if b.Tools.PermissionMode == "" {
			b.Tools.PermissionMode = "plan" // unknown role: judge nothing, touch nothing
		}
	}

	// The envelope is checked against the COMPOSED bundle, not against the
	// layers, because widening is what a layer would attempt and the merged
	// result is the only place an attempt shows. A layer naming an endpoint
	// the envelope excludes does not narrow the run; it refuses it.
	if env != nil {
		if err := env.Check(b); err != nil {
			return Bundle{}, err
		}
	}

	b.Ref = ref(b)
	return b, nil
}

// envelope decides the data handling in force for one request: the
// origination's declaration, intersected with the ceiling.
//
// Both nil is today's state everywhere — no classification is declared, and
// the resolver imposes none. A ceiling with no declaration is the mis-scoped
// request the whole rule exists for, and it is refused: absence means minimum,
// the same way an empty grant set can do nothing and an unset rollout means
// one.
func (c *Composer) envelope(req Request) (*Envelope, error) {
	if req.Envelope == nil {
		if c.Ceiling == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: this box declares a ceiling and this work declares no envelope; unclassified is refused, never defaulted", ErrEnvelope)
	}
	env := *req.Envelope
	if c.Ceiling != nil {
		narrowed, err := env.Narrow(*c.Ceiling)
		if err != nil {
			return nil, err
		}
		env = narrowed
	} else if !env.Declared() {
		return nil, fmt.Errorf("%w: an envelope naming no class declares nothing; unclassified is refused, never defaulted", ErrEnvelope)
	}
	return &env, nil
}

// Explain renders a bundle for inspection: the layers that composed it and the
// merged result, deterministically, so two bundles diff line by line the way
// a rendered manifest does.
func (c *Composer) Explain(_ context.Context, b Bundle) ([]byte, error) {
	var w strings.Builder
	fmt.Fprintf(&w, "agentref: %s\ntenant: %s\nrole: %s\nbox: %s\nmodel: %s\npermission-mode: %s\n",
		b.Ref, b.Tenant, b.Role, b.Box, b.Model, b.Tools.PermissionMode)
	envelope := "(none declared)"
	if b.Envelope != nil {
		envelope = b.Envelope.String()
	}
	fmt.Fprintf(&w, "envelope: %s\n", envelope)
	fmt.Fprintf(&w, "\nlayers (in application order):\n")
	for _, l := range b.Layers {
		fmt.Fprintf(&w, "  %-6s %-20s %s %s\n", l.Scope, l.Name, l.Source.Kind, l.Source.Digest)
	}
	fmt.Fprintf(&w, "\ntools:\n  allow: %v\n  deny:  %v\n", b.Tools.Allow, b.Tools.Deny)
	fmt.Fprintf(&w, "\nskills:\n")
	for _, s := range b.Skills {
		fmt.Fprintf(&w, "  %s @ %s\n", s.Name, s.Source.Digest)
	}
	fmt.Fprintf(&w, "\nplugins:\n")
	for _, p := range b.Plugins {
		pin := p.Commit
		if pin == "" {
			pin = "unpinned"
		}
		fmt.Fprintf(&w, "  %s %s @ %s mcp=%v %s\n", p.ID(), p.Version, pin, p.MCP, p.Path)
	}
	fmt.Fprintf(&w, "\nmcp:\n")
	for _, m := range b.MCP {
		fmt.Fprintf(&w, "  %s tools=%v\n", m.Name, m.Tools)
	}
	fmt.Fprintf(&w, "\ncontext:\n")
	for _, r := range b.Context.Repos {
		mode := "ro"
		if r.Write {
			mode = "rw"
		}
		fmt.Fprintf(&w, "  %s @ %s %s class=%s paths=%v\n", r.Repo, r.Commit, mode, r.Class, r.Paths)
	}
	fmt.Fprintf(&w, "  knowledge: %v\n", b.Context.Knowledge)
	fmt.Fprintf(&w, "\ninstructions:\n%s\n", b.Instructions)
	return []byte(w.String()), nil
}

// ref content-addresses the whole resolved bundle with the same
// length-prefixed scheme evidence uses, and for the same reason: determinism
// is load-bearing, and JSON's is too easy to lose silently.
func ref(b Bundle) string {
	h := sha256.New()
	put := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(resolveDomain)
	put(string(b.Tenant))
	put(string(b.Role))
	put(b.Box)
	put(b.Model)
	put(b.Instructions)
	put(b.Tools.PermissionMode)
	for _, a := range b.Tools.Allow {
		put("allow:" + a)
	}
	for _, d := range b.Tools.Deny {
		put("deny:" + d)
	}
	for _, s := range b.Skills {
		put("skill:" + s.Name + ":" + s.Source.Digest)
	}
	for _, p := range b.Plugins {
		mcp := "nomcp"
		if p.MCP {
			mcp = "mcp"
		}
		put("plugin:" + p.ID() + ":" + p.Version + ":" + p.Commit + ":" + p.Path + ":" + mcp)
	}
	for _, m := range b.MCP {
		put("mcp:" + m.Name + ":" + m.URL + ":" + strings.Join(m.Command, "\x00") + ":" + strings.Join(m.Tools, "\x00"))
	}
	if b.Envelope != nil {
		put("envelope:" + b.Envelope.String())
	}
	for _, r := range b.Context.Repos {
		mode := "ro"
		if r.Write {
			mode = "rw"
		}
		put("repo:" + r.Repo + "@" + r.Commit + ":" + mode + ":" + string(r.Class) + ":" + strings.Join(r.Paths, "\x00"))
	}
	for _, k := range b.Context.Knowledge {
		put("knowledge:" + k)
	}
	for _, l := range b.Layers {
		put("layer:" + string(l.Scope) + ":" + l.Name + ":" + l.Source.Digest)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Permits reports whether a tool may be used under this policy, with the
// reason a denial carries to the stream. Deny wins over allow; an empty allow
// list is no opinion rather than "nothing".
func (p ToolPolicy) Permits(tool string) (bool, string) {
	if slices.Contains(p.Deny, tool) {
		return false, fmt.Sprintf("tool %q is denied by policy", tool)
	}
	if len(p.Allow) > 0 && !slices.Contains(p.Allow, tool) {
		return false, fmt.Sprintf("tool %q is outside the allowed set %v", tool, p.Allow)
	}
	return true, ""
}
