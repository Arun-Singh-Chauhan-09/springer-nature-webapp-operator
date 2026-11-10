package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	platformv1 "webapp-operator/api/v1"
)

// Platform-level metrics, served on the operator's /metrics endpoint next to
// controller-runtime's built-in controller_runtime_reconcile_* metrics.
var (
	webappReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "webapp_ready",
		Help: "1 if the WebApp is Ready, 0 otherwise.",
	}, []string{"namespace", "name"})

	webappReplicas = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "webapp_replicas",
		Help: "Desired and ready replicas per WebApp.",
	}, []string{"namespace", "name", "kind"})

	webappPhase = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "webapp_phase",
		Help: "Current phase of each WebApp (1 for the active phase).",
	}, []string{"namespace", "name", "phase"})
)

var allPhases = []string{
	platformv1.PhaseProgressing, platformv1.PhaseReady,
	platformv1.PhaseDegraded, platformv1.PhaseError,
}

func init() {
	ctrlmetrics.Registry.MustRegister(webappReady, webappReplicas, webappPhase)
}

func recordMetrics(app *platformv1.WebApp) {
	ns, name := app.Namespace, app.Name
	ready := 0.0
	if app.Status.Phase == platformv1.PhaseReady {
		ready = 1
	}
	webappReady.WithLabelValues(ns, name).Set(ready)
	webappReplicas.WithLabelValues(ns, name, "desired").Set(float64(desiredReplicas(app)))
	webappReplicas.WithLabelValues(ns, name, "ready").Set(float64(app.Status.ReadyReplicas))
	for _, p := range allPhases {
		v := 0.0
		if p == app.Status.Phase {
			v = 1
		}
		webappPhase.WithLabelValues(ns, name, p).Set(v)
	}
}

func forgetMetrics(ns, name string) {
	webappReady.DeleteLabelValues(ns, name)
	webappReplicas.DeleteLabelValues(ns, name, "desired")
	webappReplicas.DeleteLabelValues(ns, name, "ready")
	for _, p := range allPhases {
		webappPhase.DeleteLabelValues(ns, name, p)
	}
}
