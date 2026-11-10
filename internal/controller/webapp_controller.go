package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1 "webapp-operator/api/v1"
)

// While a WebApp is not Ready we re-check periodically, because pod-level
// failures (e.g. ImagePullBackOff) do not change the Deployment we watch.
const notReadyRequeue = 10 * time.Second

// Container waiting reasons that will not fix themselves.
var failingReasons = map[string]bool{
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"InvalidImageName":           true,
	"CrashLoopBackOff":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
}

// WebAppReconciler turns WebApp resources into Deployments and Services.
type WebAppReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=platform.example.com,resources=webapps,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.com,resources=webapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is idempotent: it can run any number of times and always
// converges the cluster toward the WebApp spec.
func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var app platformv1.WebApp
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if apierrors.IsNotFound(err) {
			// Children are garbage-collected via ownerReferences.
			forgetMetrics(req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !app.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	original := app.DeepCopy()

	dep, err := r.reconcileDeployment(ctx, &app)
	if err == nil {
		err = r.reconcileService(ctx, &app)
	}
	if err != nil {
		logger.Error(err, "reconcile failed")
		r.Recorder.Event(&app, corev1.EventTypeWarning, "ReconcileFailed", err.Error())
		app.Status.Phase = platformv1.PhaseError
		setCondition(&app, platformv1.ConditionReady, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		_ = r.patchStatus(ctx, original, &app)
		recordMetrics(&app)
		return ctrl.Result{}, err
	}

	if err := r.computeStatus(ctx, &app, dep); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.patchStatus(ctx, original, &app); err != nil {
		return ctrl.Result{}, err
	}
	recordMetrics(&app)

	if app.Status.Phase != platformv1.PhaseReady {
		return ctrl.Result{RequeueAfter: notReadyRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) reconcileDeployment(ctx context.Context, app *platformv1.WebApp) (*appsv1.Deployment, error) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		mutateDeployment(app, dep)
		return controllerutil.SetControllerReference(app, dep, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("deployment: %w", err)
	}
	r.recordChildChange(app, "Deployment", op)
	return dep, nil
}

func (r *WebAppReconciler) reconcileService(ctx context.Context, app *platformv1.WebApp) error {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		mutateService(app, svc)
		return controllerutil.SetControllerReference(app, svc, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("service: %w", err)
	}
	r.recordChildChange(app, "Service", op)
	return nil
}

func (r *WebAppReconciler) recordChildChange(app *platformv1.WebApp, kind string, op controllerutil.OperationResult) {
	switch op {
	case controllerutil.OperationResultCreated:
		r.Recorder.Eventf(app, corev1.EventTypeNormal, "Created", "Created %s %s", kind, app.Name)
	case controllerutil.OperationResultUpdated:
		r.Recorder.Eventf(app, corev1.EventTypeNormal, "Updated", "Updated %s %s", kind, app.Name)
	}
}

// computeStatus derives phase and conditions from the live Deployment and pods.
func (r *WebAppReconciler) computeStatus(ctx context.Context, app *platformv1.WebApp, dep *appsv1.Deployment) error {
	want := desiredReplicas(app)
	app.Status.ObservedGeneration = app.Generation
	app.Status.ReadyReplicas = dep.Status.ReadyReplicas

	rolledOut := dep.Status.ObservedGeneration >= dep.Generation &&
		dep.Status.UpdatedReplicas == want &&
		dep.Status.ReadyReplicas == want &&
		dep.Status.AvailableReplicas == want

	switch {
	case rolledOut:
		app.Status.Phase = platformv1.PhaseReady
		setCondition(app, platformv1.ConditionReady, metav1.ConditionTrue, "AllReplicasReady",
			fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, want))
	default:
		reason, msg, err := r.findFailure(ctx, app, dep)
		if err != nil {
			return err
		}
		if reason != "" {
			app.Status.Phase = platformv1.PhaseDegraded
			setCondition(app, platformv1.ConditionReady, metav1.ConditionFalse, reason, msg)
		} else {
			app.Status.Phase = platformv1.PhaseProgressing
			setCondition(app, platformv1.ConditionReady, metav1.ConditionFalse, "RollingOut",
				fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, want))
		}
	}

	if app.Spec.NeedsDatabase {
		setCondition(app, platformv1.ConditionDatabaseReady, metav1.ConditionUnknown, "NotYetSupported",
			"database provisioning (CloudNativePG) is on the roadmap")
	} else {
		meta.RemoveStatusCondition(&app.Status.Conditions, platformv1.ConditionDatabaseReady)
	}
	return nil
}

// findFailure returns a reason when the rollout is stuck for a reason that
// will not resolve on its own, so developers see *why* instead of waiting.
func (r *WebAppReconciler) findFailure(ctx context.Context, app *platformv1.WebApp, dep *appsv1.Deployment) (string, string, error) {
	for _, c := range dep.Status.Conditions {
		if c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse &&
			c.Reason == "ProgressDeadlineExceeded" {
			return "ProgressDeadlineExceeded", c.Message, nil
		}
	}

	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(app.Namespace), client.MatchingLabels(selectorFor(app))); err != nil {
		return "", "", fmt.Errorf("list pods: %w", err)
	}
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			if w := cs.State.Waiting; w != nil && failingReasons[w.Reason] {
				return w.Reason, fmt.Sprintf("pod %s: %s", p.Name, w.Message), nil
			}
		}
	}
	return "", "", nil
}

func (r *WebAppReconciler) patchStatus(ctx context.Context, original, app *platformv1.WebApp) error {
	if err := r.Status().Patch(ctx, app, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patch status: %w", err)
	}
	return nil
}

func setCondition(app *platformv1.WebApp, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: app.Generation,
	})
}

// SetupWithManager wires the controller: it watches WebApps and the
// Deployments/Services it owns, so drift or deletion triggers a reconcile.
func (r *WebAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1.WebApp{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("webapp").
		Complete(r)
}
