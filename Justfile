# dagmar controller — Phase 0 build/dev recipes (ADR-0012 self-bootstrap trajectory).
#
# NOTE: controller-gen must run under GOWORK=off — its packages.Load trips on the parent
# go.work (/home/denkhaus/dev/gomodules/go.work). The dagmar module itself builds with the
# default workspace; only the controller-gen recipes need GOWORK=off.

controller-tools-version := "v0.21.0"
bin-dir := justfile_directory() / "bin"

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

# install CRDs into the current cluster
install: manifests
    kustomize build config/crd | kubectl apply -f -

# apply the sample Project(dagmar-own) + Run into the current cluster
apply-samples:
    kubectl apply -f config/samples/

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

# deploy the controller into the kind cluster (Increment 2)
deploy img="dagmar/controller:dev": docker-build
    -kind load docker-image {{img}} --name dagmar
    cd config/manager && kustomize edit set image controller={{img}}
    kustomize build config/default | kubectl apply -f -
