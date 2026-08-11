package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"github.com/denkhaus/dagmar/internal/controller"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// All CRD types are registered via AddToScheme (Agent, Prompt, QualityGate, Workflow
	// are registered in their init() functions).
}

func main() {
	var metricsAddr string
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics server bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health/ready probe bind address")
	zapOpts := zap.Options{Development: true}
	zapOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	// controller-runtime v0.24 requires an explicit logger (ctrl.SetLogger) or it drops all logs
	// and spams a priorityqueue stack trace at boot. Use the zap logger shipped with
	// controller-runtime (Kubebuilder idiom).
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	// Lean manager (ADR-0012 §3): no leader election, no webhook server, no conversion.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupFatal(err, "new manager")
	}

	// Collector HTTP server (ADR-0027 D3): receives step-result pushes from the
	// CognitionRun pipeline's Collector. Runs as a manager Runnable. Created before
	// the RunReconciler so the reconciler can generate per-Run push tokens.
	collector := &controller.CollectorServer{Client: mgr.GetClient()}
	if err := mgr.Add(collector); err != nil {
		setupFatal(err, "setup CollectorServer")
	}

	// collectorURL is the endpoint the agent pod pushes step results to. Read
	// from env (e.g. http://dagmar-controller.dagmar-system.svc:8082/step-result).
	// Empty = standalone (pipeline runs without pushing results).
	collectorURL := os.Getenv("DAGMAR_COLLECTOR_URL")

	if err = (&controller.RunReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Collector:    collector,
		CollectorURL: collectorURL,
	}).SetupWithManager(mgr); err != nil {
		setupFatal(err, "setup RunReconciler")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupFatal(err, "add healthz check")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupFatal(err, "add readyz check")
	}

	ctrl.Log.Info("starting dagmar-controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupFatal(err, "start manager")
	}
}

func setupFatal(err error, what string) {
	fmt.Fprintf(os.Stderr, "dagmar-controller: %s: %v\n", what, err)
	os.Exit(1)
}
