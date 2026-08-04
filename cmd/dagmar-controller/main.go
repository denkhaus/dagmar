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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics server bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health/ready probe bind address")
	flag.Parse()

	// Lean manager (ADR-0012 §3): no leader election, no webhook server, no conversion.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupFatal(err, "new manager")
	}

	if err = (&controller.RunReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
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
