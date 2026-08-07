// dagmar — autonomous Dagger/Kubernetes multi-agent system.
//
// This is dagmar's own Dagger module (seed cbb8 spike: engine tenancy & Run concurrency).
// It prototypes dagmar's Hybrid-C topology hermetically: an in-cluster Dagger engine
// serving agent pods, brought up inside an isolated k3s cluster that itself runs inside
// the OUTER Dagger engine (Docker Desktop). The outer engine runs this module; the inner
// engine (deployed into k3s) is the system under test. Never touches the production
// netcup cluster.

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dagger/dagmar/internal/app"
	"dagger/dagmar/internal/dagger"
	"dagger/dagmar/internal/domain"
	"github.com/denkhaus/dagmar/manifest"
)

// ManifestContractVersion is the manifest-contract version this platform build conforms to
// (the published contract github.com/denkhaus/dagmar/manifest, dagmar-a1e0). CRD-analogous:
// the platform declares which manifest schema revision it validates and speaks. Keeping the
// platform dependent on the published contract (not a project-local copy) is what makes the
// manifest genuinely platform-authority (ADR-0014 GAP-1, resolved by dagmar-a1e0).
const ManifestContractVersion = manifest.Version

// Dagmar is dagmar's main Dagger object (auto-named from the module). It is the primary
// entry point into dagmar's Dagger functionality AND the per-Project binding seam: the New
// constructor binds the target Project + os-eco configuration once, and every method
// (Run, Sandbox, Gate, ...) reuses that bound state (ADR-0010 §5).
type Dagmar struct {
	// Project is the source directory of the target Project dagmar operates on
	// (per-Project binding). *dagger.Directory, not *dagger.Workspace: Workspace-as-input
	// is unsupported by engine v0.21.8's codegen ("cannot code-generate ... unsupported
	// types"), and a Directory is the version-independent representation of a project
	// source tree (the SDK itself maps Workspace -> Directory; ADR-0010 §5).
	// +private
	Project *dagger.Directory
	// OsEco binds the os-eco backing services (seeds/mulch/canopy store paths) per-Project.
	// +private
	OsEco OsEcoBinding
}

// OsEcoBinding is the per-Project binding for the os-eco backing services (ADR-0005):
// store paths for seeds (issues), mulch (memory), and canopy (prompts).
type OsEcoBinding struct {
	// Seeds is the seeds issue-store path for this Project.
	Seeds string
	// Mulch is the mulch expertise-store path for this Project.
	Mulch string
	// Canopy is the canopy prompt-store path for this Project.
	Canopy string
}

// New is dagmar's constructor (ADR-0010 §5). Its arguments bind the per-Project context
// (the Project source + os-eco configuration) that every method reuses. All arguments are
// optional so the infra/spike methods (Up, DeployEngine, Probe) remain callable without a
// Project binding — they ignore the bound state.
//
// The os-eco paths are passed as primitives (not an OsEcoBinding struct) because v0.21.8's
// codegen skips a custom struct used directly as a constructor arg; OsEcoBinding remains the
// grouped field, populated here (ADR-0010 §5).
func New(
	// The target Project's source directory (per-Project binding seam).
	// +optional
	project *dagger.Directory,
	// seeds issue-store path for the Project (os-eco binding, per-Project).
	// +optional
	seeds string,
	// mulch expertise-store path for the Project (os-eco binding, per-Project).
	// +optional
	mulch string,
	// canopy prompt-store path for the Project (os-eco binding, per-Project).
	// +optional
	canopy string,
) *Dagmar {
	return &Dagmar{Project: project, OsEco: OsEcoBinding{Seeds: seeds, Mulch: mulch, Canopy: canopy}}
}

// Sandbox realizes an isolated, credentialed execution slot (a Dagger Container — Tier A,
// used directly; ADR-0010 §3). This is the v0 vertical proving the layout seams (functional
// core -> app Tier-A-direct -> main delegation -> a chainable custom return object) without
// an LLM call. Delegates to app.BuildSandbox.
//
// NOTE: the args are primitives (not a domain.SandboxSpec) because Dagger cannot code-generate
// for a foreign (non-main-package) input type. The pure domain.SandboxSpec is constructed at
// this seam from the primitives; domain stays Dagger-free and unit-tested (ADR-0010 §3).
func (m *Dagmar) Sandbox(
	// Base OCI image for the Sandbox container.
	image string,
	// Working directory inside the Sandbox (empty = image default). Named workingDir, not
	// workdir, to avoid a CLI flag collision with *dagger.Container's own workdir field.
	// +optional
	workingDir string,
) (*Sandbox, error) {
	ctr, err := app.BuildSandbox(domain.SandboxSpec{Image: image, Workdir: workingDir})
	if err != nil {
		return nil, err
	}
	return &Sandbox{ctr: ctr}, nil
}

// Sandbox is the Dagger object returned by Dagmar.Sandbox — a thin, chainable wrapper over
// the realized Container. Exported methods on it become callable Dagger functions.
type Sandbox struct {
	// +private
	ctr *dagger.Container
}

// Container returns the underlying Dagger Container (Tier A).
func (s *Sandbox) Container() *dagger.Container {
	return s.ctr
}

const engineLabel = "name=dagger-dagger-helm-engine"

// waitForAPI polls the k3s API until it answers. k3s writes its kubeconfig before the API
// server is ready to serve, so callers must wait before hitting it with helm/kubectl.
func waitForAPI(ctx context.Context, k *dagger.K3S) error {
	var lastErr error
	for range 40 {
		if _, err := k.Kubectl("get nodes").Stdout(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("k3s API not ready after 40 attempts: %w", lastErr)
}

// deployEngine installs the singleton Dagger engine into the k3s cluster (helm) and waits
// for the engine pod to be Ready. Returns the engine pod name. The engine is the cbb8
// system-under-test: a Dagger engine nested inside k3s-in-Dagger.
func deployEngine(ctx context.Context, k *dagger.K3S) (string, error) {
	helmOut, err := dag.Container().
		From("alpine/helm:3.14.0").
		WithEnvVariable("KUBECONFIG", "/.kube/config").
		WithFile("/.kube/config", k.Config()).
		WithExec([]string{
			"helm", "upgrade", "--install", "--create-namespace",
			"--namespace", "dagmar",
			"--set", "image.tag=v0.21.8",
			"--set", "privileged=true",
			"dagger", "oci://registry.dagger.io/dagger-helm",
		}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("helm install dagger engine: %w", err)
	}
	if !strings.Contains(helmOut, "STATUS: deployed") {
		return "", fmt.Errorf("helm did not report deployed")
	}
	// helm applies the manifest without waiting; `kubectl rollout status` waits for the
	// DaemonSet rollout (creates the pod and waits until Ready — handles the
	// pod-not-yet-exists timing that `kubectl wait` on a condition does not).
	if _, werr := k.Kubectl("rollout status daemonset/dagger-dagger-helm-engine -n dagmar --timeout=300s").Stdout(ctx); werr != nil {
		desc, _ := k.Kubectl("describe pod -n dagmar -l " + engineLabel).Stdout(ctx)
		logs, _ := k.Kubectl("logs -n dagmar -l " + engineLabel + " --tail=30").Stdout(ctx)
		return "", fmt.Errorf("engine rollout not ready: %w\n--- describe ---\n%s\n--- logs ---\n%s", werr, desc, logs)
	}
	pod, err := k.Kubectl("get pods -n dagmar -l " + engineLabel + " -o jsonpath={.items[0].metadata.name}").Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("get engine pod name: %w", err)
	}
	return strings.TrimSpace(pod), nil
}

// Up brings up an isolated k3s cluster inside Dagger and proves the API is reachable.
//
// First checkpoint of the cbb8 spike: validates that k3s-in-Dagger works on this host
// (nested privileges / cgroup v2) before we deploy the Dagger engine DaemonSet into it.
func (m *Dagmar) Up(ctx context.Context,
	// name of the throwaway k3s cluster
	// +optional
	// +default="dagmar-spike"
	cluster string,
) (string, error) {
	// KeepState: true keeps /var/lib/rancher/k3s (incl. the containerd image store) on the
	// persisted Dagger cache volume for this cluster name — so the ~500 MB engine image is
	// pulled ONCE and reused across calls (same cluster name). Without it, k3s wipes state
	// per start and re-pulls every time.
	k := dag.K3S(cluster, dagger.K3SOpts{KeepState: true})
	if _, err := k.Server().Start(ctx); err != nil {
		return "", fmt.Errorf("start k3s server %q: %w", cluster, err)
	}
	if err := waitForAPI(ctx, k); err != nil {
		return "", err
	}
	return k.Kubectl("get nodes -o wide").Stdout(ctx)
}

// DeployEngine deploys the singleton Dagger engine as a privileged DaemonSet into k3s and
// reports the Ready engine pod. (cbb8 spike — the nesting test.)
func (m *Dagmar) DeployEngine(ctx context.Context,
	// name of the throwaway k3s cluster
	// +optional
	// +default="dagmar-spike"
	cluster string,
) (string, error) {
	// KeepState: true keeps /var/lib/rancher/k3s (incl. the containerd image store) on the
	// persisted Dagger cache volume for this cluster name — so the ~500 MB engine image is
	// pulled ONCE and reused across calls (same cluster name). Without it, k3s wipes state
	// per start and re-pulls every time.
	k := dag.K3S(cluster, dagger.K3SOpts{KeepState: true})
	if _, err := k.Server().Start(ctx); err != nil {
		return "", fmt.Errorf("start k3s server %q: %w", cluster, err)
	}
	if err := waitForAPI(ctx, k); err != nil {
		return "", err
	}
	pod, err := deployEngine(ctx, k)
	if err != nil {
		return "", err
	}
	pods, _ := k.Kubectl("get pods -n dagmar -o wide").Stdout(ctx)
	return fmt.Sprintf("engine pod %s (Ready)\n%s", pod, pods), nil
}

// Probe validates Research Q3: can a Dagger CLIENT reach the singleton (nested) engine via
// kube-pod://? Deploys the engine, then runs `dagger core version` in a client container
// pointed at the inner engine through _EXPERIMENTAL_DAGGER_RUNNER_HOST=kube-pod://... A
// version reported from the inner engine proves the singleton engine serves clients — the
// precondition for multi-tenancy on one engine.
func (m *Dagmar) Probe(ctx context.Context,
	// name of the throwaway k3s cluster
	// +optional
	// +default="dagmar-spike"
	cluster string,
) (string, error) {
	// KeepState: true keeps /var/lib/rancher/k3s (incl. the containerd image store) on the
	// persisted Dagger cache volume for this cluster name — so the ~500 MB engine image is
	// pulled ONCE and reused across calls (same cluster name). Without it, k3s wipes state
	// per start and re-pulls every time.
	k := dag.K3S(cluster, dagger.K3SOpts{KeepState: true})
	if _, err := k.Server().Start(ctx); err != nil {
		return "", fmt.Errorf("start k3s server %q: %w", cluster, err)
	}
	if err := waitForAPI(ctx, k); err != nil {
		return "", err
	}
	pod, err := deployEngine(ctx, k)
	if err != nil {
		return "", err
	}
	runnerHost := "kube-pod://" + pod + "?namespace=dagmar"
	// kube-pod:// shells out to kubectl, so the client container needs BOTH the dagger CLI
	// and kubectl (the engine image lacks kubectl). Install both on alpine.
	out, err := dag.Container().
		From("alpine:3.20").
		WithExec([]string{"sh", "-c", "apk add --no-cache kubectl curl && DAGGER_VERSION=0.21.8 curl -fsSL https://dl.dagger.io | sh"}).
		WithEnvVariable("KUBECONFIG", "/.kube/config").
		WithFile("/.kube/config", k.Config()).
		WithEnvVariable("_EXPERIMENTAL_DAGGER_RUNNER_HOST", runnerHost).
		WithExec([]string{"dagger", "core", "version"}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("kube-pod:// probe failed (runner-host %s): %w", runnerHost, err)
	}
	return fmt.Sprintf("runner-host: %s\ninner-engine: %s", runnerHost, out), nil
}

// ProbeNet is the dagmar-911b trust-zone spike: it empirically tests whether a Dagger
// container exec has outbound network access by default. Dagger v0.21.8 exposes NO per-exec
// no-network option (ContainerWithExecOpts has no network/egress field). This establishes
// the residual-risk fact that ADR-0011 consciously accepts: tool-set exclusion is NOT a hard
// network guarantee (a raw exec path can still reach the network). LLM-free.
// (cbb8-style spike; to be refactored into a platform workflows/ package if/when one is
// introduced — the gate-family workflows/ moved to the .dagmar project module at ADR-0014.)
func (m *Dagmar) ProbeNet(ctx context.Context) (string, error) {
	// End-to-end reachability (DNS + TCP + HTTP) to a stable, low-risk endpoint from inside
	// a container exec — the same kind of exec a hermetic LLM Loop / checkable would run.
	// Alpine ships busybox wget (no apk install needed); --timeout bounds the wait. The exit
	// code is captured directly (not via a pipe, which would mask it as head's exit).
	const cmd = "wget -qO /tmp/out --timeout=5 https://example.com 2>/tmp/err; " +
		"code=$?; echo \"HTTP_FETCH_EXIT=$code\"; " +
		"echo \"--- body (first 120B) ---\"; head -c 120 /tmp/out; echo; " +
		"echo \"--- stderr (first 200B) ---\"; head -c 200 /tmp/err"
	out, err := dag.Container().
		From("alpine:3.20").
		WithExec([]string{"sh", "-c", cmd}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("ProbeNet exec failed: %w", err)
	}
	verdict := "NET_NONE — container exec has NO outbound network (egress already denied at engine/pod level)"
	if strings.Contains(out, "HTTP_FETCH_EXIT=0") {
		verdict = "NET_OK — container exec HAS outbound network by default, and Dagger v0.21.8 has no per-exec no-network flag. Tool-set exclusion is therefore NOT a hard network guarantee (a raw exec path can still reach the network); this residual risk is consciously accepted (ADR-0011) — primary control = tailored tool-set, blast radius bounded by ADR-0007."
	}
	return fmt.Sprintf("=== dagmar-911b ProbeNet (container-exec outbound test) ===\n%s\n=== VERDICT: %s ===\n", out, verdict), nil
}

// ProbeCache is the dagmar-d8f0 spike: it empirically validates that a Dagger engine isolates
// cache by volume NAME (the ADR-0008 §3 design assumption — "Dagger isolates cache by volume
// name; cache poisoning across Projects is prevented as long as Projects use distinct
// cache-volume names"). It is run as THREE separate `dagger call probe-cache --mode ...`
// invocations — i.e. three separate client sessions against one engine, the cheapest faithful
// analogue of "two client pods on the singleton engine":
//
//	dagger call probe-cache --mode write     # write MARKER_A into cache volume "…-A"
//	dagger call probe-cache --mode readsame  # read volume "…-A"  (expect MARKER_A → shares)
//	dagger call probe-cache --mode readdiff  # read volume "…-B"  (expect EMPTY   → isolates)
//
// If readsame sees the marker AND readdiff does not, name-based isolation is CONFIRMED (the
// ADR-0008 §3 claim holds locally); the remaining cross-Project concern is then purely the
// controller's allocation of distinct names (a control-plane guarantee). LLM-free.
// (cbb8/d8f0-style spike; to be refactored into a platform workflows/ package if/when one is
// introduced — the gate-family workflows/ moved to the .dagmar project module at ADR-0014.)
func (m *Dagmar) ProbeCache(ctx context.Context,
	// which leg of the test to run: write | readsame | readdiff
	mode string,
) (string, error) {
	const (
		volA   = "dagmar-probecache-A"
		volB   = "dagmar-probecache-B"
		marker = "MARKER_A"
	)
	mk := func(vol string) *dagger.Container {
		// The volume NAME is always the cache identity key (in every sharing mode); SHARED is
		// only the concurrency semantics for concurrent access. SHARED (the production default)
		// is chosen here deliberately: observing isolation under the MOST permissive mode is the
		// strongest case, not the weakest.
		return dag.Container().
			From("alpine:3.20").
			WithMountedCache("/cache", dag.CacheVolume(vol))
	}
	switch mode {
	case "write":
		out, err := mk(volA).
			WithExec([]string{"sh", "-c", "echo " + marker + " > /cache/marker.txt; cat /cache/marker.txt"}).
			Stdout(ctx)
		if err != nil {
			return "", fmt.Errorf("probe-cache write: %w", err)
		}
		return fmt.Sprintf("WROTE %q into cache volume %q\n%s", marker, volA, strings.TrimSpace(out)), nil
	case "readsame":
		out, err := mk(volA).
			WithExec([]string{"sh", "-c", "cat /cache/marker.txt 2>/dev/null || echo EMPTY"}).
			Stdout(ctx)
		if err != nil {
			return "", fmt.Errorf("probe-cache readsame: %w", err)
		}
		seen := strings.TrimSpace(out)
		verdict := "SHARES (same name sees the marker) — positive control OK"
		if seen != marker {
			verdict = "NO-SHARE (same name did NOT see the marker) — cache is not keyed/persisting by name; test invalid"
		}
		return fmt.Sprintf("READ cache volume %q -> %q\nVERDICT: %s", volA, seen, verdict), nil
	case "readdiff":
		out, err := mk(volB).
			WithExec([]string{"sh", "-c", "cat /cache/marker.txt 2>/dev/null || echo EMPTY"}).
			Stdout(ctx)
		if err != nil {
			return "", fmt.Errorf("probe-cache readdiff: %w", err)
		}
		seen := strings.TrimSpace(out)
		verdict := "ISOLATED (different name does NOT see the marker) — ADR-0008 §3 CONFIRMED"
		if seen == marker {
			verdict = "LEAK (different name SAW the marker) — ADR-0008 §3 FALSIFIED: cache isolation by name is broken"
		}
		return fmt.Sprintf("READ cache volume %q -> %q\nVERDICT: %s", volB, seen, verdict), nil
	default:
		return "", fmt.Errorf("probe-cache: unknown mode %q (want write|readsame|readdiff)", mode)
	}
}
