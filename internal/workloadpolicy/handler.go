// Package workloadpolicy is the validating admission webhook that applies the
// platform policy (internal/policy) to workloads at `kubectl apply` time.
//
// Pod Security Admission only checks Pods, so a dangerous Deployment is accepted and
// its pods then fail to appear. This webhook rejects the Deployment itself, with the
// Trivy check IDs and a fix hint, and also covers what PSA does not (image tags, limits).
package workloadpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"webapp-operator/internal/policy"
)

var (
	decisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "webapp_policy_admission_total",
		Help: "Workload admission decisions by mode (enforce|warn) and decision (allowed|warned|denied).",
	}, []string{"mode", "decision"})
	findingsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "webapp_policy_findings_total",
		Help: "Policy findings seen at admission, by Trivy check ID and action.",
	}, []string{"check", "action"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(decisions, findingsTotal)
}

// Handler validates workloads. Enforce=true denies high-risk findings; Enforce=false
// (namespaces labelled policy=warn) only returns warnings, for gradual rollout.
type Handler struct {
	Enforce bool
}

var _ admission.Handler = &Handler{}

func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	mode := "warn"
	if h.Enforce {
		mode = "enforce"
	}
	spec, skip, err := podSpecFrom(req)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if skip != "" {
		return admission.Allowed(skip)
	}

	findings := policy.Check(spec)
	for _, f := range findings {
		findingsTotal.WithLabelValues(f.ID, string(f.Action)).Inc()
	}
	deny, warn := policy.Split(findings)

	if h.Enforce && len(deny) > 0 {
		decisions.WithLabelValues(mode, "denied").Inc()
		log.FromContext(ctx).Info("workload denied by policy", "kind", req.Kind.Kind,
			"namespace", req.Namespace, "name", req.Name, "findings", strings.Join(strs(deny), "; "))
		resp := admission.Denied(fmt.Sprintf(
			"denied by platform policy (Trivy KSV checks): %s. Fix the manifest, or use a WebApp, which applies secure defaults",
			strings.Join(strs(deny), "; ")))
		resp.Warnings = strs(warn)
		return resp
	}

	warnings := strs(warn)
	if !h.Enforce {
		warnings = strs(findings) // warn mode: report everything, block nothing
	}
	decision := "allowed"
	if len(warnings) > 0 {
		decision = "warned"
	}
	decisions.WithLabelValues(mode, decision).Inc()
	resp := admission.Allowed("")
	resp.Warnings = warnings
	return resp
}

// podSpecFrom extracts the pod template from supported workload kinds. A non-empty
// skip reason means the object is allowed without checks.
func podSpecFrom(req admission.Request) (*corev1.PodSpec, string, error) {
	raw := req.Object.Raw
	switch req.Kind.Kind {
	case "Deployment":
		var o appsv1.Deployment
		return &o.Spec.Template.Spec, "", json.Unmarshal(raw, &o)
	case "StatefulSet":
		var o appsv1.StatefulSet
		return &o.Spec.Template.Spec, "", json.Unmarshal(raw, &o)
	case "DaemonSet":
		var o appsv1.DaemonSet
		return &o.Spec.Template.Spec, "", json.Unmarshal(raw, &o)
	case "Job":
		var o batchv1.Job
		return &o.Spec.Template.Spec, "", json.Unmarshal(raw, &o)
	case "CronJob":
		var o batchv1.CronJob
		return &o.Spec.JobTemplate.Spec.Template.Spec, "", json.Unmarshal(raw, &o)
	case "Pod":
		var o corev1.Pod
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, "", err
		}
		if len(o.OwnerReferences) > 0 {
			// Created by a controller whose template was already checked.
			return nil, "pod owned by a controller, template already checked", nil
		}
		return &o.Spec, "", nil
	default:
		return nil, "kind not covered by the workload policy", nil
	}
}

func strs(fs []policy.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}
