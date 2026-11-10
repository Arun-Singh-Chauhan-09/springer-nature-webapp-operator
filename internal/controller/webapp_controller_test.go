package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1 "webapp-operator/api/v1"
)

const ns = "demo"

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := platformv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newApp() *platformv1.WebApp {
	return &platformv1.WebApp{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: ns, Generation: 1},
		Spec: platformv1.WebAppSpec{
			Image:      "ghcr.io/stefanprodan/podinfo:6.7.1",
			Replicas:   ptr.To(int32(2)),
			Port:       9898,
			HealthPath: "/healthz",
		},
	}
}

func setup(t *testing.T, objs ...client.Object) (*WebAppReconciler, client.Client) {
	t.Helper()
	s := newScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&platformv1.WebApp{}, &appsv1.Deployment{}).
		Build()
	return &WebAppReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(100)}, c
}

func reconcileOnce(t *testing.T, r *WebAppReconciler) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: "shop"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

func getApp(t *testing.T, c client.Client) *platformv1.WebApp {
	t.Helper()
	var app platformv1.WebApp
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "shop"}, &app); err != nil {
		t.Fatal(err)
	}
	return &app
}

func getDeployment(t *testing.T, c client.Client) *appsv1.Deployment {
	t.Helper()
	var dep appsv1.Deployment
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "shop"}, &dep); err != nil {
		t.Fatalf("deployment: %v", err)
	}
	return &dep
}

func TestCreatesChildrenWithPlatformDefaults(t *testing.T) {
	r, c := setup(t, newApp())
	reconcileOnce(t, r)

	dep := getDeployment(t, c)
	if got := *dep.Spec.Replicas; got != 2 {
		t.Errorf("replicas = %d, want 2", got)
	}
	if len(dep.OwnerReferences) != 1 || dep.OwnerReferences[0].Kind != "WebApp" {
		t.Errorf("deployment must be owned by the WebApp, got %+v", dep.OwnerReferences)
	}

	ct := dep.Spec.Template.Spec.Containers[0]
	if ct.Resources.Limits.Memory().IsZero() || ct.Resources.Requests.Cpu().IsZero() {
		t.Error("default resource requests/limits were not applied")
	}
	if ct.ReadinessProbe == nil || ct.ReadinessProbe.HTTPGet.Path != "/healthz" {
		t.Error("readiness probe missing or wrong path")
	}
	if ct.LivenessProbe == nil {
		t.Error("liveness probe missing")
	}
	if ct.SecurityContext == nil || *ct.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container must disallow privilege escalation")
	}
	if psc := dep.Spec.Template.Spec.SecurityContext; psc == nil || !*psc.RunAsNonRoot {
		t.Error("pod must run as non-root")
	}
	if dep.Spec.Template.Labels["app.kubernetes.io/managed-by"] != managedBy {
		t.Error("observability labels missing on pod template")
	}

	var svc corev1.Service
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "shop"}, &svc); err != nil {
		t.Fatalf("service: %v", err)
	}
	if svc.Spec.Ports[0].Port != 9898 {
		t.Errorf("service port = %d, want 9898", svc.Spec.Ports[0].Port)
	}
}

func TestStatusProgressingThenReady(t *testing.T) {
	r, c := setup(t, newApp())

	res := reconcileOnce(t, r)
	app := getApp(t, c)
	if app.Status.Phase != platformv1.PhaseProgressing {
		t.Fatalf("phase = %q, want Progressing", app.Status.Phase)
	}
	if res.RequeueAfter == 0 {
		t.Error("a not-ready WebApp should be requeued")
	}

	// Simulate the Deployment controller finishing the rollout.
	dep := getDeployment(t, c)
	dep.Status = appsv1.DeploymentStatus{
		ObservedGeneration: dep.Generation,
		Replicas:           2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2,
	}
	if err := c.Status().Update(context.Background(), dep); err != nil {
		t.Fatal(err)
	}

	reconcileOnce(t, r)
	app = getApp(t, c)
	if app.Status.Phase != platformv1.PhaseReady {
		t.Fatalf("phase = %q, want Ready", app.Status.Phase)
	}
	if !meta.IsStatusConditionTrue(app.Status.Conditions, platformv1.ConditionReady) {
		t.Error("Ready condition should be True")
	}
	if app.Status.ReadyReplicas != 2 {
		t.Errorf("readyReplicas = %d, want 2", app.Status.ReadyReplicas)
	}
}

func TestDegradedOnImagePullBackOff(t *testing.T) {
	app := newApp()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "shop-abc", Namespace: ns, Labels: selectorFor(app)},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: containerName,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason: "ImagePullBackOff", Message: "image not found",
			}},
		}}},
	}
	r, c := setup(t, app, pod)
	reconcileOnce(t, r)

	got := getApp(t, c)
	if got.Status.Phase != platformv1.PhaseDegraded {
		t.Fatalf("phase = %q, want Degraded", got.Status.Phase)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, platformv1.ConditionReady)
	if cond == nil || cond.Reason != "ImagePullBackOff" {
		t.Errorf("Ready condition reason = %+v, want ImagePullBackOff", cond)
	}
}

func TestSelfHealsDeletedDeployment(t *testing.T) {
	r, c := setup(t, newApp())
	reconcileOnce(t, r)

	if err := c.Delete(context.Background(), getDeployment(t, c)); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r)
	getDeployment(t, c) // fails the test if it was not recreated
}

func TestRevertsManualDrift(t *testing.T) {
	r, c := setup(t, newApp())
	reconcileOnce(t, r)

	dep := getDeployment(t, c)
	dep.Spec.Replicas = ptr.To(int32(7))
	dep.Spec.Template.Spec.Containers[0].Image = "evil:1"
	if err := c.Update(context.Background(), dep); err != nil {
		t.Fatal(err)
	}

	reconcileOnce(t, r)
	dep = getDeployment(t, c)
	if *dep.Spec.Replicas != 2 || dep.Spec.Template.Spec.Containers[0].Image != newApp().Spec.Image {
		t.Error("operator did not revert manual drift")
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	r, c := setup(t, newApp())
	reconcileOnce(t, r)
	before := getDeployment(t, c).ResourceVersion

	reconcileOnce(t, r)
	reconcileOnce(t, r)
	if after := getDeployment(t, c).ResourceVersion; after != before {
		t.Errorf("deployment was rewritten without a spec change (rv %s -> %s)", before, after)
	}
}

func TestDatabaseConditionReported(t *testing.T) {
	app := newApp()
	app.Spec.NeedsDatabase = true
	r, c := setup(t, app)
	reconcileOnce(t, r)

	cond := meta.FindStatusCondition(getApp(t, c).Status.Conditions, platformv1.ConditionDatabaseReady)
	if cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Errorf("DatabaseReady condition = %+v, want Unknown", cond)
	}
}

func TestDeletedWebAppIsIgnored(t *testing.T) {
	r, _ := setup(t)
	reconcileOnce(t, r) // must not error when the WebApp is gone
}
