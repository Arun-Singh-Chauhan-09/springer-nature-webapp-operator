package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	platformv1 "webapp-operator/api/v1"
)

// Platform defaults. These are the "paved road": every WebApp gets them
// without the developer having to know they exist.
const (
	containerName           = "app"
	portName                = "http"
	defaultHealthPath       = "/"
	defaultRunAsUser        = int64(10001)
	progressDeadlineSeconds = int32(120)
	managedBy               = "webapp-operator"
)

func labelsFor(app *platformv1.WebApp) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       app.Name,
		"app.kubernetes.io/instance":   app.Name,
		"app.kubernetes.io/managed-by": managedBy,
	}
}

// selectorFor is a strict subset of labelsFor; selectors are immutable,
// so they must never change once the Deployment exists.
func selectorFor(app *platformv1.WebApp) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     app.Name,
		"app.kubernetes.io/instance": app.Name,
	}
}

func desiredReplicas(app *platformv1.WebApp) int32 {
	if app.Spec.Replicas != nil {
		return *app.Spec.Replicas
	}
	return 1
}

func healthPath(app *platformv1.WebApp) string {
	if app.Spec.HealthPath != "" {
		return app.Spec.HealthPath
	}
	return defaultHealthPath
}

func defaultResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func resourcesFor(app *platformv1.WebApp) corev1.ResourceRequirements {
	if app.Spec.Resources != nil {
		return *app.Spec.Resources.DeepCopy()
	}
	return defaultResources()
}

func probe(app *platformv1.WebApp, initialDelay int32) *corev1.Probe {
	// Every field is set explicitly so that the API server's defaulting
	// never produces a diff and causes needless updates.
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   healthPath(app),
				Port:   intstr.FromString(portName),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		InitialDelaySeconds: initialDelay,
		TimeoutSeconds:      1,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
}

// mutateDeployment brings dep in line with the WebApp. It edits the object
// in place (rather than replacing the spec) so that fields defaulted by the
// API server survive and CreateOrUpdate only writes on a real change.
func mutateDeployment(app *platformv1.WebApp, dep *appsv1.Deployment) {
	labels := labelsFor(app)

	if dep.Labels == nil {
		dep.Labels = map[string]string{}
	}
	for k, v := range labels {
		dep.Labels[k] = v
	}

	if dep.CreationTimestamp.IsZero() {
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorFor(app)}
	}
	dep.Spec.Replicas = ptr.To(desiredReplicas(app))
	dep.Spec.ProgressDeadlineSeconds = ptr.To(progressDeadlineSeconds)

	tpl := &dep.Spec.Template
	if tpl.Labels == nil {
		tpl.Labels = map[string]string{}
	}
	for k, v := range labels {
		tpl.Labels[k] = v
	}

	tpl.Spec.AutomountServiceAccountToken = ptr.To(false)
	tpl.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		RunAsUser:    ptr.To(defaultRunAsUser),
		RunAsGroup:   ptr.To(defaultRunAsUser),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	var c *corev1.Container
	for i := range tpl.Spec.Containers {
		if tpl.Spec.Containers[i].Name == containerName {
			c = &tpl.Spec.Containers[i]
			break
		}
	}
	if c == nil {
		tpl.Spec.Containers = append(tpl.Spec.Containers, corev1.Container{Name: containerName})
		c = &tpl.Spec.Containers[len(tpl.Spec.Containers)-1]
	}

	c.Image = app.Spec.Image
	c.Ports = []corev1.ContainerPort{{
		Name:          portName,
		ContainerPort: app.Spec.Port,
		Protocol:      corev1.ProtocolTCP,
	}}
	c.Resources = resourcesFor(app)
	c.ReadinessProbe = probe(app, 2)
	c.LivenessProbe = probe(app, 10)
	c.SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		Privileged:               ptr.To(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

// mutateService exposes the app inside the cluster.
func mutateService(app *platformv1.WebApp, svc *corev1.Service) {
	if svc.Labels == nil {
		svc.Labels = map[string]string{}
	}
	for k, v := range labelsFor(app) {
		svc.Labels[k] = v
	}
	svc.Spec.Type = corev1.ServiceTypeClusterIP
	svc.Spec.Selector = selectorFor(app)
	svc.Spec.Ports = []corev1.ServicePort{{
		Name:       portName,
		Port:       app.Spec.Port,
		TargetPort: intstr.FromString(portName),
		Protocol:   corev1.ProtocolTCP,
	}}
}
