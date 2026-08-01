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

	"dagger/dagmar/internal/dagger"
)

type Dagmar struct{}

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
