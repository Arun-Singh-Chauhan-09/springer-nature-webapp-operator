package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"webapp-operator/internal/policy"
)

// The paved road must pass the platform's own admission policy: every Deployment the
// operator generates for a WebApp has zero deny findings. If someone weakens a default
// in defaults.go, this test fails before the webhook would start rejecting WebApps.
func TestGeneratedDeploymentPassesWorkloadPolicy(t *testing.T) {
	dep := &appsv1.Deployment{}
	mutateDeployment(newApp(), dep)

	deny, warn := policy.Split(policy.Check(&dep.Spec.Template.Spec))
	if len(deny) > 0 {
		t.Fatalf("operator defaults violate the policy: %v", deny)
	}
	t.Logf("remaining warnings (allowed): %v", warn)
}
