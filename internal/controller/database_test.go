package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newCluster() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(cnpgClusterGVK)
	return u
}

func TestMutateClusterOnCreate(t *testing.T) {
	app := newApp()
	app.Spec.NeedsDatabase = true
	u := newCluster()
	if err := mutateCluster(app, u, DatabaseConfig{Enabled: true, BackupObjectStore: "platform-backups"}); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := unstructured.NestedInt64(u.Object, "spec", "instances"); v != 1 {
		t.Errorf("instances = %d, want 1", v)
	}
	if v, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "size"); v != "1Gi" {
		t.Errorf("storage size = %q, want default 1Gi", v)
	}
	if v, _, _ := unstructured.NestedString(u.Object, "spec", "bootstrap", "initdb", "database"); v != "app" {
		t.Errorf("initdb database = %q", v)
	}
	plugins, _, _ := unstructured.NestedSlice(u.Object, "spec", "plugins")
	if len(plugins) != 1 {
		t.Fatalf("expected the Barman Cloud plugin for WAL archiving, got %v", plugins)
	}
	p := plugins[0].(map[string]any)
	if p["name"] != barmanCloudPlugin || p["isWALArchiver"] != true {
		t.Errorf("plugin = %v", p)
	}
	if u.GetLabels()["app.kubernetes.io/managed-by"] != managedBy {
		t.Errorf("labels = %v", u.GetLabels())
	}
}

func TestMutateClusterWithoutBackups(t *testing.T) {
	u := newCluster()
	if err := mutateCluster(newApp(), u, DatabaseConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedSlice(u.Object, "spec", "plugins"); found {
		t.Error("no backup object store configured, so no plugin should be set")
	}
}

// After creation only instances is reconciled: fields CNPG defaults or changes on
// the server must not be overwritten, or every reconcile would trigger an update.
func TestMutateClusterAfterCreateOnlyTouchesInstances(t *testing.T) {
	u := newCluster()
	u.SetCreationTimestamp(metav1.Now())
	_ = unstructured.SetNestedField(u.Object, "5Gi", "spec", "storage", "size")
	if err := mutateCluster(newApp(), u, DatabaseConfig{Enabled: true, BackupObjectStore: "x"}); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "size"); v != "5Gi" {
		t.Errorf("storage size overwritten to %q", v)
	}
	if _, found, _ := unstructured.NestedSlice(u.Object, "spec", "plugins"); found {
		t.Error("plugins must not be added to an existing cluster")
	}
}

func TestClusterState(t *testing.T) {
	u := newCluster()
	_ = unstructured.SetNestedField(u.Object, int64(1), "spec", "instances")
	if s := clusterState(u); s.ready || s.reason != "Provisioning" {
		t.Errorf("new cluster: %+v", s)
	}
	_ = unstructured.SetNestedField(u.Object, cnpgHealthyPhase, "status", "phase")
	_ = unstructured.SetNestedField(u.Object, int64(1), "status", "readyInstances")
	if s := clusterState(u); !s.ready {
		t.Errorf("healthy cluster not ready: %+v", s)
	}
}

func TestDatabaseURLOnlyInjectedWhenReady(t *testing.T) {
	app := newApp()
	app.Spec.NeedsDatabase = true
	dep := &appsv1.Deployment{}
	mutateDeployment(app, dep)

	injectDatabaseEnv(app, dep, dbState{ready: false})
	if env := dep.Spec.Template.Spec.Containers[0].Env; len(env) != 0 {
		t.Fatalf("DATABASE_URL injected before the database was ready: %v", env)
	}

	injectDatabaseEnv(app, dep, dbState{ready: true})
	env := dep.Spec.Template.Spec.Containers[0].Env
	if len(env) != 1 || env[0].Name != "DATABASE_URL" ||
		env[0].ValueFrom.SecretKeyRef.Name != "shop-db-app" || env[0].ValueFrom.SecretKeyRef.Key != "uri" {
		t.Fatalf("unexpected env: %+v", env)
	}

	injectDatabaseEnv(app, dep, dbState{ready: true}) // idempotent
	if n := len(dep.Spec.Template.Spec.Containers[0].Env); n != 1 {
		t.Fatalf("DATABASE_URL duplicated: %d env vars", n)
	}
}
