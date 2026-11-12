// Package policy checks a PodSpec against the platform's workload security rules.
//
// The rules follow the Trivy Kubernetes checks (KSV IDs) used in my MEng thesis
// "Security and Deployability of LLM-Generated Kubernetes Manifests", where the
// scanners' findings decided whether a manifest was MISCONFIGURED or CLEAN. Here the
// same checks run at admission time: high-risk findings are denied, hygiene gaps are
// returned as warnings so the author sees them without being blocked.
package policy

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Action is what admission does with a finding.
type Action string

const (
	Deny Action = "deny"
	Warn Action = "warn"
)

// Finding is one rule violation.
type Finding struct {
	ID        string // Trivy check ID, e.g. KSV017
	Action    Action
	Container string // empty for pod-level findings
	Message   string
}

func (f Finding) String() string {
	if f.Container != "" {
		return fmt.Sprintf("[%s] container %q: %s", f.ID, f.Container, f.Message)
	}
	return fmt.Sprintf("[%s] %s", f.ID, f.Message)
}

// Check returns every finding for the pod spec, pod-level first, then per container.
func Check(spec *corev1.PodSpec) []Finding {
	var out []Finding
	pod := spec.SecurityContext

	if spec.HostNetwork {
		out = append(out, Finding{ID: "KSV009", Action: Deny, Message: "uses the host network (hostNetwork: true)"})
	}
	if spec.HostPID {
		out = append(out, Finding{ID: "KSV010", Action: Deny, Message: "shares the host PID namespace (hostPID: true)"})
	}
	if spec.HostIPC {
		out = append(out, Finding{ID: "KSV008", Action: Deny, Message: "shares the host IPC namespace (hostIPC: true)"})
	}
	for _, v := range spec.Volumes {
		if v.HostPath != nil {
			out = append(out, Finding{ID: "KSV023", Action: Deny, Message: fmt.Sprintf("mounts hostPath %q as volume %q", v.HostPath.Path, v.Name)})
		}
	}

	containers := append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...)
	for _, c := range containers {
		out = append(out, checkContainer(c, pod)...)
	}
	return out
}

func checkContainer(c corev1.Container, pod *corev1.PodSecurityContext) []Finding {
	var out []Finding
	add := func(id string, a Action, msg string) {
		out = append(out, Finding{ID: id, Action: a, Container: c.Name, Message: msg})
	}
	sc := c.SecurityContext
	if sc == nil {
		sc = &corev1.SecurityContext{}
	}

	if sc.Privileged != nil && *sc.Privileged {
		add("KSV017", Deny, "runs privileged (privileged: true)")
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		add("KSV001", Deny, "can escalate privileges (set allowPrivilegeEscalation: false)")
	}
	if !runsAsNonRoot(sc, pod) {
		add("KSV012", Deny, "may run as root (set runAsNonRoot: true and a non-zero runAsUser)")
	}
	if sc.Capabilities != nil {
		for _, capability := range sc.Capabilities.Add {
			switch strings.ToUpper(string(capability)) {
			case "SYS_ADMIN":
				add("KSV005", Deny, "adds the SYS_ADMIN capability")
			case "NET_BIND_SERVICE":
				// allowed by the restricted Pod Security Standard
			default:
				add("KSV022", Warn, fmt.Sprintf("adds the non-default capability %s", capability))
			}
		}
	}
	if !dropsAll(sc) {
		add("KSV003", Warn, "does not drop all capabilities (capabilities.drop: [ALL])")
	}
	if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
		add("KSV014", Warn, "root filesystem is writable (set readOnlyRootFilesystem: true)")
	}
	if !hasSeccomp(sc, pod) {
		add("KSV030", Warn, "no seccomp profile (set seccompProfile.type: RuntimeDefault)")
	}
	if _, ok := c.Resources.Limits[corev1.ResourceCPU]; !ok {
		add("KSV011", Warn, "no CPU limit")
	}
	if _, ok := c.Resources.Limits[corev1.ResourceMemory]; !ok {
		add("KSV018", Warn, "no memory limit")
	}
	if !pinnedImage(c.Image) {
		add("KSV013", Deny, fmt.Sprintf("image %q uses :latest or no tag (pin a version or digest)", c.Image))
	}
	return out
}

// Split separates findings into the ones that block admission and the warnings.
func Split(findings []Finding) (deny, warn []Finding) {
	for _, f := range findings {
		if f.Action == Deny {
			deny = append(deny, f)
		} else {
			warn = append(warn, f)
		}
	}
	return deny, warn
}

func runsAsNonRoot(sc *corev1.SecurityContext, pod *corev1.PodSecurityContext) bool {
	user := sc.RunAsUser
	nonRoot := sc.RunAsNonRoot
	if pod != nil {
		if user == nil {
			user = pod.RunAsUser
		}
		if nonRoot == nil {
			nonRoot = pod.RunAsNonRoot
		}
	}
	if user != nil && *user == 0 {
		return false
	}
	return nonRoot != nil && *nonRoot
}

func dropsAll(sc *corev1.SecurityContext) bool {
	if sc.Capabilities == nil {
		return false
	}
	for _, d := range sc.Capabilities.Drop {
		if strings.EqualFold(string(d), "ALL") {
			return true
		}
	}
	return false
}

func hasSeccomp(sc *corev1.SecurityContext, pod *corev1.PodSecurityContext) bool {
	p := sc.SeccompProfile
	if p == nil && pod != nil {
		p = pod.SeccompProfile
	}
	return p != nil && p.Type != corev1.SeccompProfileTypeUnconfined
}

// pinnedImage is true for a digest or an explicit tag other than "latest".
// "registry:5000/app" has no tag: the port belongs to the registry host.
func pinnedImage(image string) bool {
	if strings.Contains(image, "@") {
		return true
	}
	name := image[strings.LastIndex(image, "/")+1:]
	i := strings.LastIndex(name, ":")
	if i < 0 {
		return false
	}
	return name[i+1:] != "latest" && name[i+1:] != ""
}
