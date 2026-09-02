package harness

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// SkillResolver pins skill refs to the exact trees they name. Implemented by
// the git mirror, which is the only thing here that can reach a remote.
type SkillResolver interface {
	ResolveSkills(ctx context.Context, refs []agent.SkillRef) ([]agent.SkillRef, error)
}

// Source is an [agent.LayerSource] that contributes a box's agentic surface —
// plugins, pinned skills, granted MCP servers — beneath whatever base layers
// the box already has.
//
// Resolution happens on every Layers call, which is to say once per run, and
// that is the whole design rather than an implementation detail. A plugin set
// captured at daemon start is a plugin set that drifts from the machine
// silently; resolved per run it cannot, because the run reads the same files
// the agent about to start will read. The cost is bounded: plugin discovery is
// two small JSON files, and a skill already in the mirror at its pinned commit
// needs no network at all.
type Source struct {
	// Base layers apply first, most general first.
	Base []agent.Layer
	// Box names the box this surface belongs to.
	Box string
	// Inherit reads the machine's enabled plugins into the layer. False makes
	// the configuration the only authority.
	Inherit bool
	// PluginMCP reports plugins as carrying their own MCP servers. See
	// [agent.PluginRef.MCP] for why the default is off.
	PluginMCP bool
	// ConfigDir overrides the harness config directory; empty resolves
	// through [ConfigDir].
	ConfigDir string
	// Skills are unresolved refs from configuration; Resolver pins them.
	Skills   []agent.SkillRef
	Resolver SkillResolver
	// MCP are the servers this box grants.
	MCP []agent.MCPRef
}

// Layers returns the base layers followed by the resolved harness layer.
//
// A resolution failure fails the run. The alternative — carrying on with a
// skill the bundle promised and the agent never got — is the divergence the
// agentref exists to make impossible, and it is invisible from the outside: an
// agent that was never given a skill and an agent that chose not to use one
// produce identical evidence.
func (s *Source) Layers(ctx context.Context, req agent.Request) ([]agent.Layer, error) {
	layers := make([]agent.Layer, 0, len(s.Base)+1)
	layers = append(layers, s.Base...)

	l := agent.Layer{
		Name:  "harness",
		Scope: agent.ScopeBox,
		MCP:   s.MCP,
	}
	if s.Inherit {
		plugins, err := Discovery{Dir: s.ConfigDir, MCP: s.PluginMCP}.Plugins()
		if err != nil {
			return nil, err
		}
		l.Plugins = plugins
	}
	if len(s.Skills) > 0 {
		if s.Resolver == nil {
			return nil, fmt.Errorf("harness: box %s pins %d skills but has no resolver to pin them with", s.Box, len(s.Skills))
		}
		skills, err := s.Resolver.ResolveSkills(ctx, s.Skills)
		if err != nil {
			return nil, err
		}
		l.Skills = skills
	}
	if len(l.Plugins) == 0 && len(l.Skills) == 0 && len(l.MCP) == 0 {
		// Nothing to say. An empty layer would still change the agentref
		// through its own digest, which would make "this box grants nothing"
		// and "this box was never asked" different agents on the record.
		return layers, nil
	}
	l.Source = agent.Source{Kind: agent.SourceInline, Digest: digest(l)}
	return append(layers, l), nil
}

// digest content-addresses the resolved surface with the same length-prefixed
// scheme the bundle ref uses, and for the same reason: the layer's digest is
// hashed into the agentref, so it has to change when — and only when — the
// material does.
func digest(l agent.Layer) string {
	h := sha256.New()
	put := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put("project-box/harness/v1")
	for _, p := range l.Plugins {
		mcp := "nomcp"
		if p.MCP {
			mcp = "mcp"
		}
		put("plugin:" + p.ID() + ":" + p.Version + ":" + p.Commit + ":" + p.Path + ":" + mcp)
	}
	for _, sk := range l.Skills {
		put("skill:" + sk.Name + ":" + sk.Source.Repo + ":" + sk.Source.Commit + ":" + sk.Source.Path + ":" + sk.Source.Digest)
	}
	for _, m := range l.MCP {
		put("mcp:" + m.Name + ":" + m.URL)
		for _, a := range m.Command {
			put("arg:" + a)
		}
		for _, t := range m.Tools {
			put("tool:" + t)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
