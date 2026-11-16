package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

var rel002 = check{
	id: "REL-002",
	meta: Meta{
		Category: Reliability,
		Severity: High,
		Title:    "Containers without memory limits",
		Description: "A container with no memory limit can grow until the node itself runs out of memory. " +
			"When that happens Kubernetes kills pods on that node, often ones that had nothing to do with the leak.",
		Remediation: "Set a memory limit on every container. Start at roughly 1.5 to 2 times the memory request and adjust from real usage.",
		Snippet: `resources:
  requests:
    memory: 256Mi
  limits:
    memory: 512Mi`,
	},
	run: func(s *collect.Snapshot) Result {
		kinds := []string{KindDeployment, KindStatefulSet, KindDaemonSet, KindCronJob, KindPod}
		return containerCheck(s, kinds, func(c corev1.Container) string {
			if _, ok := c.Resources.Limits[corev1.ResourceMemory]; !ok {
				return "no memory limit"
			}
			return ""
		})
	},
}
