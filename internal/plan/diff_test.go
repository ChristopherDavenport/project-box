package plan

import (
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	base := []Node{
		{ID: "one", Task: "do one"},
		{ID: "two", Task: "do two", DependsOn: []string{"one"}, Sections: []string{"a", "b"}},
	}
	clone := func(nodes []Node) []Node {
		out := make([]Node, len(nodes))
		for i, n := range nodes {
			out[i] = n
			out[i].DependsOn = append([]string(nil), n.DependsOn...)
			out[i].Sections = append([]string(nil), n.Sections...)
		}
		return out
	}

	cases := []struct {
		name string
		a, b []Node
		want Delta
	}{
		{
			name: "identical plans",
			a:    clone(base), b: clone(base),
			want: Delta{},
		},
		{
			name: "add-only",
			a:    clone(base),
			b:    append(clone(base), Node{ID: "three", Task: "do three"}),
			want: Delta{Added: []Node{{ID: "three", Task: "do three"}}},
		},
		{
			name: "remove-only",
			a:    append(clone(base), Node{ID: "three", Task: "do three"}),
			b:    clone(base),
			want: Delta{Removed: []Node{{ID: "three", Task: "do three"}}},
		},
		{
			name: "a changed task",
			a:    clone(base),
			b: func() []Node {
				n := clone(base)
				n[0].Task = "do one differently"
				return n
			}(),
			want: Delta{Changed: []NodeChange{{
				ID:     "one",
				Before: Node{ID: "one", Task: "do one"},
				After:  Node{ID: "one", Task: "do one differently"},
			}}},
		},
		{
			name: "DependsOn and Sections order shuffled alone is not a change",
			a: []Node{
				{ID: "one", Task: "do one"}, {ID: "two", Task: "do two"},
				{ID: "three", Task: "do three", DependsOn: []string{"one", "two"}, Sections: []string{"a", "b"}},
			},
			b: []Node{
				{ID: "one", Task: "do one"}, {ID: "two", Task: "do two"},
				{ID: "three", Task: "do three", DependsOn: []string{"two", "one"}, Sections: []string{"b", "a"}},
			},
			want: Delta{},
		},
		{
			name: "node order shuffled between the two plans",
			a:    []Node{base[1], base[0]},
			b:    []Node{base[0], base[1]},
			want: Delta{},
		},
		{
			// A re-justified edge is a different judgement even when the edge
			// itself — which node depends on which — is unchanged.
			name: "reason-only change on an unchanged edge is a change",
			a: []Node{
				{ID: "one", Task: "do one"},
				{ID: "two", Task: "do two", DependsOn: []string{"one"}, EdgeReasons: map[string]string{"one": "uses Foo()"}},
			},
			b: []Node{
				{ID: "one", Task: "do one"},
				{ID: "two", Task: "do two", DependsOn: []string{"one"}, EdgeReasons: map[string]string{"one": "uses Bar()"}},
			},
			want: Delta{Changed: []NodeChange{{
				ID:     "two",
				Before: Node{ID: "two", Task: "do two", DependsOn: []string{"one"}, EdgeReasons: map[string]string{"one": "uses Foo()"}},
				After:  Node{ID: "two", Task: "do two", DependsOn: []string{"one"}, EdgeReasons: map[string]string{"one": "uses Bar()"}},
			}}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Plan{Nodes: c.a}
			b := Plan{Nodes: c.b}
			got := Diff(a, b)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Diff(a, b) = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestDiffIsSymmetricOnOrderAlone: shuffling which plan is "a" and which is
// "b" when the graphs are the same set produces the identical (zero) delta
// either way — a diff over node order would be noise, and noise is what
// [Plan.Seal] already refuses to let the ref carry.
func TestDiffIsSymmetricOnOrderAlone(t *testing.T) {
	x := []Node{{ID: "one", Task: "do one"}, {ID: "two", Task: "do two"}}
	y := []Node{{ID: "two", Task: "do two"}, {ID: "one", Task: "do one"}}
	a := Plan{Nodes: x}
	b := Plan{Nodes: y}
	if got := Diff(a, b); !reflect.DeepEqual(got, Delta{}) {
		t.Fatalf("Diff(a, b) = %+v, want zero-value Delta", got)
	}
	if got := Diff(b, a); !reflect.DeepEqual(got, Delta{}) {
		t.Fatalf("Diff(b, a) = %+v, want zero-value Delta", got)
	}
}
