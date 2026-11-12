package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformv1 "webapp-operator/api/v1"
)

// CloudNativePG is driven through unstructured objects, so the operator does not
// depend on CNPG's Go module and still builds and runs where CNPG is not installed.
var cnpgClusterGVK = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"}

const (
	barmanCloudPlugin = "barman-cloud.cloudnative-pg.io"
	databaseName      = "app"
	// CNPG's healthy phase string, see Cluster.status.phase.
	cnpgHealthyPhase = "Cluster in healthy state"
)

// DatabaseConfig is how the platform provisions databases for WebApps with
// spec.needsDatabase. It comes from operator flags.
type DatabaseConfig struct {
	// Enabled is false when CloudNativePG is not installed (plain `make up`, CI e2e).
	Enabled bool
	// BackupObjectStore is the name of a Barman Cloud ObjectStore in the app's
	// namespace. Empty disables WAL archiving and backups.
	BackupObjectStore string
	// StorageSize of the database volume, e.g. "1Gi".
	StorageSize string
}

// dbState is the database part of a WebApp's status.
type dbState struct {
	ready   bool
	reason  string
	message string
}

func dbClusterName(app *platformv1.WebApp) string { return app.Name + "-db" }

// dbSecretName is the secret CloudNativePG creates for the application user.
// Its "uri" key is a full postgresql:// connection string.
func dbSecretName(app *platformv1.WebApp) string { return dbClusterName(app) + "-app" }

// reconcileDatabase creates the app's CNPG Cluster (owned by the WebApp, so it is
// garbage-collected with it; backups in the object store are kept) and reports
// whether it is ready.
func (r *WebAppReconciler) reconcileDatabase(ctx context.Context, app *platformv1.WebApp) (dbState, error) {
	if !r.Database.Enabled {
		return dbState{reason: "DatabasesDisabled",
			message: "this platform has no database support (operator started without --enable-databases)"}, nil
	}

	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(cnpgClusterGVK)
	cluster.SetName(dbClusterName(app))
	cluster.SetNamespace(app.Namespace)

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, cluster, func() error {
		if err := mutateCluster(app, cluster, r.Database); err != nil {
			return err
		}
		return controllerutil.SetControllerReference(app, cluster, r.Scheme)
	})
	if err != nil {
		return dbState{}, fmt.Errorf("database cluster: %w", err)
	}
	r.recordChildChange(app, "database Cluster", op)

	return clusterState(cluster), nil
}

// mutateCluster sets the platform's opinion of a database. The full spec is only
// written at creation; afterwards only the instance count is reconciled, so fields
// CloudNativePG defaults on the server never cause an update loop.
func mutateCluster(app *platformv1.WebApp, cluster *unstructured.Unstructured, cfg DatabaseConfig) error {
	labels := cluster.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range labelsFor(app) {
		labels[k] = v
	}
	cluster.SetLabels(labels)

	if err := unstructured.SetNestedField(cluster.Object, int64(1), "spec", "instances"); err != nil {
		return err
	}
	if created := cluster.GetCreationTimestamp(); !created.IsZero() {
		return nil
	}

	size := cfg.StorageSize
	if size == "" {
		size = "1Gi"
	}
	spec := map[string]any{
		"storage": map[string]any{"size": size},
		"bootstrap": map[string]any{
			"initdb": map[string]any{"database": databaseName, "owner": databaseName},
		},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "500m", "memory": "512Mi"},
		},
	}
	if cfg.BackupObjectStore != "" {
		spec["plugins"] = []any{map[string]any{
			"name":          barmanCloudPlugin,
			"isWALArchiver": true,
			"parameters":    map[string]any{"barmanObjectName": cfg.BackupObjectStore},
		}}
	}
	for k, v := range spec {
		if err := unstructured.SetNestedField(cluster.Object, v, "spec", k); err != nil {
			return err
		}
	}
	return nil
}

// clusterState reads readiness from the CNPG Cluster status.
func clusterState(cluster *unstructured.Unstructured) dbState {
	phase, _, _ := unstructured.NestedString(cluster.Object, "status", "phase")
	readyInstances, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
	instances, _, _ := unstructured.NestedInt64(cluster.Object, "spec", "instances")

	if phase == cnpgHealthyPhase && instances > 0 && readyInstances >= instances {
		return dbState{ready: true, reason: "ClusterHealthy",
			message: fmt.Sprintf("Postgres %s ready (%d/%d instances)", cluster.GetName(), readyInstances, instances)}
	}
	if phase == "" {
		phase = "creating"
	}
	return dbState{reason: "Provisioning",
		message: fmt.Sprintf("Postgres %s: %s (%d/%d instances ready)", cluster.GetName(), phase, readyInstances, instances)}
}

// injectDatabaseEnv adds DATABASE_URL to the app container, but only once the
// database is ready. Injecting it earlier would start pods before CNPG has created
// the credentials secret; this way the app rolls out once, with a working URL.
func injectDatabaseEnv(app *platformv1.WebApp, dep *appsv1.Deployment, db dbState) {
	for i := range dep.Spec.Template.Spec.Containers {
		c := &dep.Spec.Template.Spec.Containers[i]
		if c.Name != containerName {
			continue
		}
		env := make([]corev1.EnvVar, 0, len(c.Env)+1)
		for _, e := range c.Env {
			if e.Name != "DATABASE_URL" {
				env = append(env, e)
			}
		}
		if app.Spec.NeedsDatabase && db.ready {
			env = append(env, corev1.EnvVar{
				Name: "DATABASE_URL",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: dbSecretName(app)},
					Key:                  "uri",
				}},
			})
		}
		if len(env) == 0 {
			env = nil
		}
		c.Env = env
	}
}
