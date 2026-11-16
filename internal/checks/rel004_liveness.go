package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var rel004 = check{
	id: "REL-004",
	meta: Meta{
		Category: Reliability,
		Severity: Medium,
		Title:    "Workloads without liveness probes",
		Description: "Without a liveness probe, a container that hangs or deadlocks keeps running. " +
			"Kubernetes believes it is healthy, so nothing restarts it until a person notices.",
		Remediation: "Add a liveness probe that fails when the process is stuck. Keep it cheap and do not make it depend on other services.",
		Snippet: `livenessProbe:
  httpGet:
    path: /livez
    port: 8080
  periodSeconds: 10
  failureThreshold: 3`,
	},
	run: func(s *collect.Snapshot) Result {
		// CronJobs are left out: batch jobs run to completion and rarely
		// benefit from a liveness probe.
		kinds := []string{KindDeployment, KindStatefulSet, KindDaemonSet, KindPod}
		return containerCheck(s, kinds, func(c corev1.Container) string {
			if c.LivenessProbe == nil {
				return "no liveness probe"
			}
			return ""
		})
	},
}
