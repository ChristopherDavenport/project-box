package render

import (
	"bytes"
	"strings"
	"testing"
)

func svc() Service {
	return Service{
		Name: "shop-api", Box: "shop", Image: "gcr.io/acme/shop-api", Tag: "v1.2.3",
		Port: 8080, Replicas: 2,
		Env: map[string]string{"B_SECOND": "2", "A_FIRST": "1", "C_THIRD": "3"},
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	tgt := Target{Env: "prod", Namespace: "shop"}
	a, err := Manifests(svc(), tgt, DefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 { // map order must never leak into the render
		b, err := Manifests(svc(), tgt, DefaultTemplate)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatal("same declaration, same template, different bytes")
		}
	}
	// Env keys land sorted.
	out := string(a)
	if strings.Index(out, "A_FIRST") > strings.Index(out, "B_SECOND") {
		t.Fatal("env keys are not sorted")
	}
	if !strings.Contains(out, `image: "gcr.io/acme/shop-api:v1.2.3"`) {
		t.Fatalf("render missing the image:\n%s", out)
	}
}

func TestTemplateChangeChangesDigest(t *testing.T) {
	tgt := Target{Env: "prod", Namespace: "shop"}
	d1, err := Digest(svc(), tgt, DefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Digest(svc(), tgt, DefaultTemplate+"\n# convention v2\n")
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatal("a template change did not move the digest — convergence campaigns would never fire")
	}
	// And an unchanged template does not.
	d3, _ := Digest(svc(), tgt, DefaultTemplate)
	if d1 != d3 {
		t.Fatal("digest is not stable")
	}
}
