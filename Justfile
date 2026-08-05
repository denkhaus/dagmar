# dagmar controller — Phase 0 build/dev recipes (ADR-0012 self-bootstrap trajectory).
#
# NOTE: controller-gen must run under GOWORK=off — its packages.Load trips on the parent
# go.work (/home/denkhaus/dev/gomodules/go.work). The dagmar module itself builds with the
# default workspace; only the controller-gen recipes need GOWORK=off.

controller-tools-version := "v0.21.0"
engine-version := "0.21.8"
bin-dir := justfile_directory() / "bin"
# gopass key holding the fine-grained PAT (contents:read on github.com/denkhaus/dagmar) used to
# fetch this private module. The PATH is not secret — the value lives encrypted in gopass. Override
# per-invocation: `just git-creds key=dev/other/path`.
git-creds-key := "dev/dagmar/github_token"

# install controller-gen into bin/ if missing (go-install-tool idiom)
controller-gen:
    @mkdir -p {{bin-dir}}
    @test -x {{bin-dir}}/controller-gen || GOBIN={{bin-dir}} go install sigs.k8s.io/controller-tools/cmd/controller-gen@{{controller-tools-version}}

# generate CRD + RBAC manifests (GOWORK=off — see header note)
manifests: controller-gen
    GOWORK=off {{bin-dir}}/controller-gen rbac:roleName=manager-role crd paths="./..." \
        output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

# generate DeepCopy methods (GOWORK=off — see header note)
generate: controller-gen
    GOWORK=off {{bin-dir}}/controller-gen object paths="./..."

# deploy the singleton Dagger engine into the current cluster (PREREQUISITE for Runs — engine
# management is NOT reconciled by the Phase-0 controller; ADR-0012 §2). cbb8 helm recipe:
# oci://registry.dagger.io/dagger-helm, image.tag=v0.21.8, privileged=true, namespace dagmar.
deploy-engine:
    helm upgrade --install --create-namespace \
        --namespace dagmar \
        --set image.tag=v{{engine-version}} \
        --set privileged=true \
        dagger oci://registry.dagger.io/dagger-helm
    kubectl rollout status daemonset/dagger-dagger-helm-engine -n dagmar --timeout=540s

# install CRDs into the current cluster
install: manifests
    kustomize build config/crd | kubectl apply -f -

# apply the sample Project(dagmar-own) + Run into the current cluster
apply-samples:
    kubectl apply -f config/samples/dagmar_v1alpha1_project.yaml -f config/samples/dagmar_v1alpha1_run.yaml

# create/replace the dagmar-git-creds Secret (the private-module PAT) in the default namespace,
# piping the token gopass→kubectl so it NEVER touches argv or shell history. Requires the gopass
# key (git-creds-key above) to hold a fine-grained PAT, contents:read on github.com/denkhaus/dagmar.
# The PAT is never committed; it is projected into the agent pod + consumed by a headless git
# credential helper (ADR-0013 §4 D10, the resolved #8805 mechanism).
git-creds key=git-creds-key:
    gopass show -o {{key}} | tr -d '\n' \
        | kubectl create secret generic dagmar-git-creds --from-file=token=/dev/stdin -n default -o yaml --dry-run=client \
        | kubectl apply -f -

# run the controller locally against the current cluster's kubeconfig
run: manifests
    go run ./cmd/dagmar-controller

# standard go checks
build:
    go build ./...
test:
    go test ./...
vet:
    go vet ./...
fmt:
    gofmt -w .

# build the manager image (for in-cluster deploy — Increment 2)
docker-build img="dagmar/controller:dev":
    docker build -t {{img}} .

# deploy the controller into the current cluster (Increment 2 — kind/docker-desktop)
deploy img="dagmar/controller:dev": docker-build
    -kind load docker-image {{img}} --name dagmar 2>/dev/null || true
    cd config/manager && kustomize edit set image controller={{img}}
    kustomize build config/default | kubectl apply -f -
