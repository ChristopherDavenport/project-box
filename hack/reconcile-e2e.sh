#!/usr/bin/env bash
# The Phase 4 reconcile-verification leg, end to end on a local kind cluster:
#
#   deployloop (the REAL internal/loop + internal/deploy code) lands an env
#   commit -> Argo CD syncs at exactly that revision -> the workload runs.
#   Then rollback: git revert -> Argo prunes the workload. No cluster
#   credentials ever appear in the control plane's path; everything after
#   the landed commit is Argo's.
#
# Two guardrails learned the hard way, encoded below:
#   - allowEmpty: automated prune refuses to delete EVERYTHING unless the
#     Application says allowEmpty — reverting the only service otherwise
#     leaves the workload standing, by design.
#   - the path must exist: git drops empty directories, and Argo fails with
#     "app path does not exist" if a revert empties services/ — so the env
#     repo seed carries services/.gitkeep.
#
# Requires: docker, kind, kubectl, go. Idempotent-ish: safe to rerun; use
# DELIVERY=release-e2e-$RANDOM for a fresh work item.
set -euo pipefail

CLUSTER=${CLUSTER:-box}
DELIVERY=${DELIVERY:-release-e2e-1}
GITEA_CRED=box:boxpass123

step() { printf '\n== %s ==\n' "$*"; }

step "cluster"
kind get clusters | grep -qx "$CLUSTER" || kind create cluster --name "$CLUSTER" --wait 120s
kubectl config use-context "kind-$CLUSTER" >/dev/null

step "gitea"
kubectl apply -f - <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata: {name: gitea, namespace: default}
spec:
  replicas: 1
  selector: {matchLabels: {app: gitea}}
  template:
    metadata: {labels: {app: gitea}}
    spec:
      containers:
        - name: gitea
          image: gitea/gitea:1.24
          env:
            - {name: GITEA__security__INSTALL_LOCK, value: "true"}
            - {name: GITEA__server__ROOT_URL, value: "http://gitea.default.svc.cluster.local:3000/"}
          ports: [{containerPort: 3000}]
---
apiVersion: v1
kind: Service
metadata: {name: gitea, namespace: default}
spec:
  selector: {app: gitea}
  ports: [{port: 3000, targetPort: 3000}]
YAML
kubectl wait --for=condition=Available deploy/gitea --timeout=300s
GPOD=$(kubectl get pod -l app=gitea -o name | head -1)
kubectl exec "$GPOD" -- su git -c \
  "gitea admin user create --username box --password boxpass123 --email box@invalid --admin --must-change-password=false" \
  2>/dev/null || true

step "argo cd"
kubectl get ns argocd >/dev/null 2>&1 || kubectl create namespace argocd
kubectl apply --server-side --force-conflicts -n argocd \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml >/dev/null
kubectl -n argocd wait --for=condition=Available deploy --all --timeout=420s
kubectl -n argocd rollout status statefulset/argocd-application-controller --timeout=300s

step "env repo"
kubectl port-forward svc/gitea 3000:3000 >/dev/null 2>&1 &
PF=$!; trap 'kill $PF 2>/dev/null || true' EXIT
sleep 3
curl -s -u "$GITEA_CRED" -X POST http://localhost:3000/api/v1/user/repos \
  -H 'Content-Type: application/json' -d '{"name":"env-prod","default_branch":"main"}' >/dev/null || true
SEED=$(mktemp -d)
git clone -q "http://$GITEA_CRED@localhost:3000/box/env-prod.git" "$SEED" 2>/dev/null || true
if [ ! -e "$SEED/services/.gitkeep" ]; then
  mkdir -p "$SEED/services"
  echo "prod env" > "$SEED/README.md"
  touch "$SEED/services/.gitkeep"
  git -C "$SEED" checkout -q -B main 2>/dev/null || true
  git -C "$SEED" add -A
  GIT_AUTHOR_NAME=box GIT_AUTHOR_EMAIL=box@invalid GIT_COMMITTER_NAME=box GIT_COMMITTER_EMAIL=box@invalid \
    git -C "$SEED" commit -qm "seed env repo (services/ kept present)" || true
  git -C "$SEED" push -q origin main || git -C "$SEED" push -q "http://$GITEA_CRED@localhost:3000/box/env-prod.git" main
fi

step "argo application"
kubectl apply -f - <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: repo-env-prod
  namespace: argocd
  labels: {argocd.argoproj.io/secret-type: repository}
stringData:
  type: git
  url: http://gitea.default.svc.cluster.local:3000/box/env-prod.git
  username: box
  password: boxpass123
  insecure: "true"
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: env-prod, namespace: argocd}
spec:
  project: default
  source:
    repoURL: http://gitea.default.svc.cluster.local:3000/box/env-prod.git
    targetRevision: main
    path: services
  destination: {server: "https://kubernetes.default.svc", namespace: shop}
  syncPolicy:
    automated: {prune: true, selfHeal: true, allowEmpty: true}
    syncOptions: [CreateNamespace=true]
YAML

step "deploy loop (the real control-plane code)"
go run ./hack/deployloop -remote "http://$GITEA_CRED@localhost:3000/box/env-prod.git" -delivery "$DELIVERY"

step "reconcile"
for i in $(seq 1 30); do
  kubectl -n argocd annotate application env-prod argocd.argoproj.io/refresh=normal --overwrite >/dev/null 2>&1 || true
  SYNC=$(kubectl -n argocd get application env-prod -o jsonpath='{.status.sync.status}')
  HEALTH=$(kubectl -n argocd get application env-prod -o jsonpath='{.status.health.status}')
  echo "  sync=$SYNC health=$HEALTH"
  [ "$SYNC" = Synced ] && [ "$HEALTH" = Healthy ] && break
  sleep 10
done
kubectl -n shop rollout status deploy/shop-api --timeout=180s
kubectl -n shop get deploy,pods -o wide

step "PASS: landed env commit reconciled into a running workload"
echo "rollback is: git revert <deploy sha> && git push  (see docs/ARCHITECTURE.md)"
