package policy

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// secureSpec is a pod spec that should produce no findings at all.
func secureSpec() *corev1.PodSpec {
	return &corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   ptr.To(true),
			RunAsUser:      ptr.To(int64(10001)),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Containers: []corev1.Container{{
			Name:  "app",
			Image: "ghcr.io/stefanprodan/podinfo:6.7.1",
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr.To(false),
				ReadOnlyRootFilesystem:   ptr.To(true),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			},
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			}},
		}},
	}
}

func has(findings []Finding, id string, a Action) bool {
	for _, f := range findings {
		if f.ID == id && f.Action == a {
			return true
		}
	}
	return false
}

func TestSecureSpecHasNoFindings(t *testing.T) {
	if f := Check(secureSpec()); len(f) != 0 {
		t.Fatalf("expected no findings, got %v", f)
	}
}

func TestRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*corev1.PodSpec)
		id     string
		action Action
	}{
		{"hostNetwork", func(s *corev1.PodSpec) { s.HostNetwork = true }, "KSV009", Deny},
		{"hostPID", func(s *corev1.PodSpec) { s.HostPID = true }, "KSV010", Deny},
		{"hostIPC", func(s *corev1.PodSpec) { s.HostIPC = true }, "KSV008", Deny},
		{"hostPath", func(s *corev1.PodSpec) {
			s.Volumes = []corev1.Volume{{Name: "root", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		}, "KSV023", Deny},
		{"privileged", func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.Privileged = ptr.To(true) }, "KSV017", Deny},
		{"privilege escalation unset", func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.AllowPrivilegeEscalation = nil }, "KSV001", Deny},
		{"runAsNonRoot unset", func(s *corev1.PodSpec) { s.SecurityContext.RunAsNonRoot = nil }, "KSV012", Deny},
		{"runAsUser 0", func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.RunAsUser = ptr.To(int64(0)) }, "KSV012", Deny},
		{"SYS_ADMIN", func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext.Capabilities.Add = []corev1.Capability{"SYS_ADMIN"}
		}, "KSV005", Deny},
		{"other added capability", func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext.Capabilities.Add = []corev1.Capability{"NET_RAW"}
		}, "KSV022", Warn},
		{"no drop ALL", func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.Capabilities = nil }, "KSV003", Warn},
		{"writable root fs", func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.ReadOnlyRootFilesystem = nil }, "KSV014", Warn},
		{"no seccomp", func(s *corev1.PodSpec) { s.SecurityContext.SeccompProfile = nil }, "KSV030", Warn},
		{"no cpu limit", func(s *corev1.PodSpec) { delete(s.Containers[0].Resources.Limits, corev1.ResourceCPU) }, "KSV011", Warn},
		{"no memory limit", func(s *corev1.PodSpec) { delete(s.Containers[0].Resources.Limits, corev1.ResourceMemory) }, "KSV018", Warn},
		{"latest tag", func(s *corev1.PodSpec) { s.Containers[0].Image = "nginx:latest" }, "KSV013", Deny},
		{"init container checked too", func(s *corev1.PodSpec) {
			s.InitContainers = []corev1.Container{{Name: "init", Image: "busybox:1.36", SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)}}}
		}, "KSV017", Deny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := secureSpec()
			tc.mutate(s)
			if f := Check(s); !has(f, tc.id, tc.action) {
				t.Fatalf("expected %s/%s, got %v", tc.id, tc.action, f)
			}
		})
	}
}

func TestPinnedImage(t *testing.T) {
	cases := map[string]bool{
		"nginx":                          false,
		"nginx:latest":                   false,
		"nginx:1.27":                     true,
		"registry:5000/app":              false, // port is not a tag
		"registry:5000/app:1.0":          true,
		"ghcr.io/org/app@sha256:0123abc": true,
	}
	for image, want := range cases {
		if got := pinnedImage(image); got != want {
			t.Errorf("pinnedImage(%q) = %v, want %v", image, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	deny, warn := Split([]Finding{{ID: "KSV017", Action: Deny}, {ID: "KSV011", Action: Warn}})
	if len(deny) != 1 || len(warn) != 1 {
		t.Fatalf("got deny=%v warn=%v", deny, warn)
	}
}
