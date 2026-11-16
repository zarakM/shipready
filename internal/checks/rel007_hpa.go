package checks

import (
	"github.com/zarakM/shipready/internal/collect"
)

var rel007 = check{
	id: "REL-007",
	meta: Meta{
		Category: Reliability,
		Severity: Low,
		Title:    "Deployments with no HorizontalPodAutoscaler",
		Description: "A Deployment with a fixed replica count cannot absorb a traffic spike. " +
			"It either falls over under load or you pay for peak capacity around the clock.",
		Remediation: "Add a HorizontalPodAutoscaler to services whose load varies. Fixed-size background workers can be left as they are.",
		Snippet: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResHPAs); reason != "" {
			return skipped(reason)
		}
		autoscalers := autoscalerTargets(s)
		return workloadCheck(s, []string{KindDeployment}, func(w workload) string {
			if w.Replicas == 0 {
				return "" // deliberately scaled to zero
			}
			if _, ok := autoscalers[w.key()]; ok {
				return ""
			}
			return "no autoscaler targets it"
		})
	},
}
