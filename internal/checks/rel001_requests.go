package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

var rel001 = check{
	id: "REL-001",
	meta: Meta{
		Category: Reliability,
		Severity: High,
		Title:    "Containers without CPU/memory requests",
		Description: "Without requests, Kubernetes does not know how much room a container needs. " +
			"It places pods on nodes blindly, so a busy neighbour can starve your service, " +
			"and these pods are the first to be evicted when a node runs short of memory.",
		Remediation: "Set a CPU and a memory request on every container, based on what it uses under normal load.",
		Snippet: `resources:
  requests:
    cpu: 250m
    memory: 256Mi`,
	},
	run: func(s *collect.Snapshot) Result {
		kinds := []string{KindDeployment, KindStatefulSet, KindDaemonSet, KindCronJob, KindPod}
		return containerCheck(s, kinds, func(c corev1.Container) string {
			cpu := hasRequest(c, corev1.ResourceCPU)
			mem := hasRequest(c, corev1.ResourceMemory)
			switch {
			case !cpu && !mem:
				return "no CPU or memory request"
			case !cpu:
				return "no CPU request"
			case !mem:
				return "no memory request"
			}
			return ""
		})
	},
}

// hasRequest treats a limit as an implied request: when a pod is created with
// a limit and no request, Kubernetes sets the request to the limit.
func hasRequest(c corev1.Container, name corev1.ResourceName) bool {
	if _, ok := c.Resources.Requests[name]; ok {
		return true
	}
	_, ok := c.Resources.Limits[name]
	return ok
}
