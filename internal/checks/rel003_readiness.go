package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var rel003 = check{
	id: "REL-003",
	meta: Meta{
		Category: Reliability,
		Severity: High,
		Title:    "Deployments/StatefulSets without readiness probes",
		Description: "Without a readiness probe, Kubernetes sends traffic to a pod the moment its process starts, " +
			"before it can actually serve. Users see errors on every deploy, restart and scale-up.",
		Remediation: "Add a readiness probe that only succeeds once the container can handle requests.",
		Snippet: `readinessProbe:
  httpGet:
    path: /healthz
    port: 8080
  periodSeconds: 5`,
	},
	run: func(s *collect.Snapshot) Result {
		kinds := []string{KindDeployment, KindStatefulSet}
		return containerCheck(s, kinds, func(c corev1.Container) string {
			if c.ReadinessProbe == nil {
				return "no readiness probe"
			}
			return ""
		})
	},
}
