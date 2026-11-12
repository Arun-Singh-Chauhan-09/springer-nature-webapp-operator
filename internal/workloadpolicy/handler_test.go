package workloadpolicy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func request(t *testing.T, kind string, obj any) admission.Request {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:       "test",
		Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: kind},
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: raw},
	}}
}

// unsafeDeployment is the kind of manifest my thesis found LLMs produce for "make it work" prompts.
func unsafeDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "demo"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			HostNetwork: true,
			Containers: []corev1.Container{{
				Name: "app", Image: "nginx:latest",
				SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)},
			}},
		}}},
	}
}

func TestEnforceDeniesUnsafeDeployment(t *testing.T) {
	resp := (&Handler{Enforce: true}).Handle(context.Background(), request(t, "Deployment", unsafeDeployment()))
	if resp.Allowed {
		t.Fatal("expected the unsafe Deployment to be denied")
	}
	for _, id := range []string{"KSV017", "KSV009", "KSV013", "KSV012"} {
		if !strings.Contains(resp.Result.Message, id) {
			t.Errorf("denial message misses %s: %s", id, resp.Result.Message)
		}
	}
	if len(resp.Warnings) == 0 {
		t.Error("expected warnings for the hygiene findings as well")
	}
}

func TestWarnModeAllowsButReports(t *testing.T) {
	resp := (&Handler{Enforce: false}).Handle(context.Background(), request(t, "Deployment", unsafeDeployment()))
	if !resp.Allowed {
		t.Fatalf("warn mode must not block: %v", resp.Result)
	}
	if len(resp.Warnings) == 0 {
		t.Fatal("warn mode must return the findings as warnings")
	}
}

func TestControllerOwnedPodIsSkipped(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "rs", UID: "1"}}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx", SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)}}}},
	}
	req := request(t, "Pod", pod)
	req.Kind.Group = ""
	if resp := (&Handler{Enforce: true}).Handle(context.Background(), req); !resp.Allowed {
		t.Fatal("pods owned by a controller must be skipped")
	}
}

func TestUnknownKindAllowed(t *testing.T) {
	if resp := (&Handler{Enforce: true}).Handle(context.Background(), request(t, "ConfigMap", map[string]string{})); !resp.Allowed {
		t.Fatal("kinds outside the policy must be allowed")
	}
}
