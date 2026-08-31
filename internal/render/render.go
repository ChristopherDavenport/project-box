// Package render derives deploy artifacts from service declarations. Nobody
// writes manifests by hand: a convention change is a template change, every
// service whose render no longer matches becomes campaign work, and drift is
// detectable because there is a canonical form to compare against.
package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"text/template"
)

// Service is a declaration: what the service is, not how it is deployed.
type Service struct {
	Name     string
	Box      string
	Image    string
	Tag      string
	Port     int
	Replicas int
	Env      map[string]string
}

// Target is one environment's placement.
type Target struct {
	Env       string
	Namespace string
}

// DefaultTemplate renders the manifests Argo CD reconciles from the env repo:
// a Deployment and a Service per service declaration. Argo watches the env
// repo path; nothing here talks to a cluster.
const DefaultTemplate = `# GENERATED — derived from the service declaration; do not edit.
# service: {{.Svc.Name}}  box: {{.Svc.Box}}  env: {{.Tgt.Env}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Svc.Name}}
  namespace: {{.Tgt.Namespace}}
  labels: {app: {{.Svc.Name}}, box: {{.Svc.Box}}}
spec:
  replicas: {{.Svc.Replicas}}
  selector: {matchLabels: {app: {{.Svc.Name}}}}
  template:
    metadata: {labels: {app: {{.Svc.Name}}}}
    spec:
      containers:
        - name: {{.Svc.Name}}
          image: "{{.Svc.Image}}:{{.Svc.Tag}}"
          ports: [{containerPort: {{.Svc.Port}}}]
{{- if .EnvKeys}}
          env:
{{- range .EnvKeys}}
            - {name: {{.}}, value: "{{index $.Svc.Env .}}"}
{{- end}}
{{- end}}
---
apiVersion: v1
kind: Service
metadata:
  name: {{.Svc.Name}}
  namespace: {{.Tgt.Namespace}}
spec:
  selector: {app: {{.Svc.Name}}}
  ports: [{port: 80, targetPort: {{.Svc.Port}}}]
`

// Manifests renders deterministically: map iteration is sorted, the template
// is pure, and the same declaration against the same template is
// byte-identical — the property drift detection and the render-digest fact
// both stand on.
func Manifests(svc Service, tgt Target, tpl string) ([]byte, error) {
	if svc.Replicas <= 0 {
		svc.Replicas = 1
	}
	keys := make([]string, 0, len(svc.Env))
	for k := range svc.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t, err := template.New("manifests").Parse(tpl)
	if err != nil {
		return nil, fmt.Errorf("render: template: %w", err)
	}
	var out bytes.Buffer
	if err := t.Execute(&out, struct {
		Svc     Service
		Tgt     Target
		EnvKeys []string
	}{svc, tgt, keys}); err != nil {
		return nil, fmt.Errorf("render: %s for %s: %w", svc.Name, tgt.Env, err)
	}
	return out.Bytes(), nil
}

// Digest fingerprints a render, for the estate fact that drives convergence
// campaigns: a service whose recorded digest no longer matches its current
// render is work.
func Digest(svc Service, tgt Target, tpl string) (string, error) {
	b, err := Manifests(svc, tgt, tpl)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
