package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var sec001 = check{
	id: "SEC-001",
	meta: Meta{
		Category: Security,
		Severity: High,
		Title:    "Containers running as root or privileged",
		Description: "A process running as root inside a container is one kernel or runtime bug away from being root on the node. " +
			"A privileged container already is: it can read every other pod's data and credentials on that node.",
		Remediation: "Set `runAsNonRoot: true` and a non-zero user on every pod, and remove `privileged: true` unless the workload truly manages the node.",
		Snippet: `securityContext:
  runAsNonRoot: true
  runAsUser: 10001
  allowPrivilegeEscalation: false`,
	},
	run: func(s *collect.Snapshot) Result {
		return checkContainers(s, allKinds, true, rootProblem)
	},
}

// rootProblem applies the container's security context on top of the pod's,
// the way Kubernetes does.
func rootProblem(w workload, c corev1.Container) string {
	var uid *int64
	var nonRoot *bool
	if psc := w.Spec.SecurityContext; psc != nil {
		uid, nonRoot = psc.RunAsUser, psc.RunAsNonRoot
	}
	if sc := c.SecurityContext; sc != nil {
		if sc.Privileged != nil && *sc.Privileged {
			return "runs privileged"
		}
		if sc.RunAsUser != nil {
			uid = sc.RunAsUser
		}
		if sc.RunAsNonRoot != nil {
			nonRoot = sc.RunAsNonRoot
		}
	}
	switch {
	case uid != nil && *uid == 0:
		return "runs as root (runAsUser: 0)"
	case uid != nil:
		return ""
	case nonRoot != nil && *nonRoot:
		return ""
	}
	return "nothing stops it running as root"
}
