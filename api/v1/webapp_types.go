package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types reported on WebApp.status.conditions.
const (
	// ConditionReady is True when all desired replicas are available.
	ConditionReady = "Ready"
	// ConditionDatabaseReady reports the state of the optional database.
	ConditionDatabaseReady = "DatabaseReady"
)

// Phases shown in `kubectl get webapp`.
const (
	PhaseProgressing = "Progressing"
	PhaseReady       = "Ready"
	PhaseDegraded    = "Degraded"
	PhaseError       = "Error"
)

// WebAppSpec is the small, developer-facing contract of the platform.
// Everything else (probes, limits, security context, labels) is filled
// in by the operator as a platform default.
type WebAppSpec struct {
	// Image is the container image, pinned to a tag (":latest" is rejected).
	Image string `json:"image"`

	// Replicas is the desired number of pods. Defaults to 1.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Port is the container port the app listens on.
	Port int32 `json:"port"`

	// HealthPath is the HTTP path used for liveness/readiness probes. Defaults to "/".
	// +optional
	HealthPath string `json:"healthPath,omitempty"`

	// NeedsDatabase requests a Postgres database for the app.
	// +optional
	NeedsDatabase bool `json:"needsDatabase,omitempty"`

	// Resources overrides the platform default requests/limits.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// WebAppStatus is what the platform reports back to the developer.
type WebAppStatus struct {
	// ObservedGeneration is the spec generation last processed by the operator.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas is the number of ready pods.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Phase is a one-word summary: Progressing, Ready, Degraded or Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions follow the standard Kubernetes condition conventions.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// WebApp is a developer-facing abstraction over Deployment + Service (+ database).
type WebApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WebAppSpec   `json:"spec,omitempty"`
	Status WebAppStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WebAppList contains a list of WebApp.
type WebAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WebApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WebApp{}, &WebAppList{})
}
