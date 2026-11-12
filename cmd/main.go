// Command manager runs the webapp-operator.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	platformv1 "webapp-operator/api/v1"
	"webapp-operator/internal/controller"
	"webapp-operator/internal/workloadpolicy"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(platformv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr, probeAddr string
	var leaderElect, enableWebhooks, enableDatabases bool
	var dbBackupStore, dbStorageSize string
	var webhookPort int
	var certDir string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address the Prometheus metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address the health probe endpoint binds to.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election so only one replica reconciles at a time.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false, "Serve the workload policy admission webhook (needs a TLS cert, e.g. from cert-manager).")
	flag.BoolVar(&enableDatabases, "enable-databases", false, "Provision CloudNativePG Postgres clusters for WebApps with needsDatabase (needs CNPG installed).")
	flag.StringVar(&dbBackupStore, "db-backup-object-store", "", "Barman Cloud ObjectStore name (in each app namespace) for WAL archiving and backups; empty disables backups.")
	flag.StringVar(&dbStorageSize, "db-storage-size", "1Gi", "Volume size for each WebApp database.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "Port the admission webhook listens on.")
	flag.StringVar(&certDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory with tls.crt and tls.key for the webhook.")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "webapp-operator.platform.example.com",
	}
	if enableWebhooks {
		mgrOpts.WebhookServer = webhook.NewServer(webhook.Options{Port: webhookPort, CertDir: certDir})
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := (&controller.WebAppReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("webapp-operator"),
		Database: controller.DatabaseConfig{
			Enabled:           enableDatabases,
			BackupObjectStore: dbBackupStore,
			StorageSize:       dbStorageSize,
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WebApp")
		os.Exit(1)
	}

	if enableWebhooks {
		// Two paths, selected by the namespace label platform.example.com/policy:
		// enforce (deny high-risk findings) or warn (report only, for gradual rollout).
		srv := mgr.GetWebhookServer()
		srv.Register("/validate-workloads-enforce", &webhook.Admission{Handler: &workloadpolicy.Handler{Enforce: true}})
		srv.Register("/validate-workloads-warn", &webhook.Admission{Handler: &workloadpolicy.Handler{Enforce: false}})
		if err := mgr.AddReadyzCheck("webhook", srv.StartedChecker()); err != nil {
			setupLog.Error(err, "unable to set up webhook ready check")
			os.Exit(1)
		}
		setupLog.Info("workload policy webhook enabled", "port", webhookPort)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager exited with error")
		os.Exit(1)
	}
}
