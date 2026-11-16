package checks

import (
	"regexp"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/zarakM/shipready/internal/collect"
)

var rel006 = check{
	id: "REL-006",
	meta: Meta{
		Category: Reliability,
		Severity: High,
		Title:    "Production workloads running a single replica",
		Description: "A service with one replica goes down every time its pod restarts, its node is replaced or a deploy rolls out. " +
			"There is no second copy to take the traffic.",
		Remediation: "Run at least two replicas of every production service. If an autoscaler manages it, set the autoscaler's minimum to 2.",
		Snippet: `spec:
  replicas: 2`,
	},
	run: func(s *collect.Snapshot) Result {
		kinds := []string{KindDeployment, KindStatefulSet}
		if reason := unavailable(s, kinds...); reason != "" {
			return skipped(reason)
		}
		if reason := s.Unavailable(collect.ResHPAs); reason != "" {
			return skipped(reason)
		}
		autoscalers := autoscalerTargets(s)

		var found []Resource
		for _, w := range workloads(s, kinds...) {
			if w.Replicas != 1 || nonProduction(w.Namespace) {
				continue
			}
			detail := "runs a single replica"
			if hpa, ok := autoscalers[w.key()]; ok {
				if replicas(hpa.Spec.MinReplicas) >= 2 {
					continue
				}
				detail = "runs a single replica (its autoscaler's minimum is 1)"
			}
			found = append(found, Resource{Namespace: w.Namespace, Kind: w.Kind, Name: w.Name, Detail: detail})
		}
		return fromResources(found)
	},
}

// nonProductionName matches namespaces that are clearly not production, such
// as "staging", "dev-team-a" or "checkout-preview".
var nonProductionName = regexp.MustCompile(`(^|-)(dev|develop|development|staging|stage|stg|test|testing|qa|sandbox|preview)(-|$)`)

func nonProduction(namespace string) bool {
	return nonProductionName.MatchString(namespace)
}

// autoscalerTargets indexes HorizontalPodAutoscalers by the workload they
// scale, using the same key as workload.key.
func autoscalerTargets(s *collect.Snapshot) map[string]*autoscalingv2.HorizontalPodAutoscaler {
	out := map[string]*autoscalingv2.HorizontalPodAutoscaler{}
	for i := range s.HPAs {
		hpa := &s.HPAs[i]
		ref := hpa.Spec.ScaleTargetRef
		out[hpa.Namespace+"/"+ref.Kind+"/"+ref.Name] = hpa
	}
	return out
}
