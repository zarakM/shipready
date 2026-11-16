package checks

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/zarakM/shipready/internal/collect"
)

var rel005 = check{
	id: "REL-005",
	meta: Meta{
		Category: Reliability,
		Severity: Medium,
		Title:    "Multi-replica workloads without a PodDisruptionBudget",
		Description: "Without a PodDisruptionBudget, a node upgrade or scale-down can take every replica of a service offline at once. " +
			"Running several replicas does not protect you during maintenance unless a budget says how many must stay up.",
		Remediation: "Create a PodDisruptionBudget for each multi-replica workload, selecting its pods by label.",
		Snippet: `apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: api
spec:
  maxUnavailable: 1
  selector:
    matchLabels:
      app: api`,
	},
	run: func(s *collect.Snapshot) Result {
		kinds := []string{KindDeployment, KindStatefulSet}
		if reason := unavailable(s, kinds...); reason != "" {
			return skipped(reason)
		}
		if reason := s.Unavailable(collect.ResPDBs); reason != "" {
			return skipped(reason)
		}

		// Selectors per namespace. A budget with an unparseable selector
		// protects nothing, so it is dropped.
		budgets := map[string][]labels.Selector{}
		for _, pdb := range s.PDBs {
			sel, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
			if err != nil {
				continue
			}
			budgets[pdb.Namespace] = append(budgets[pdb.Namespace], sel)
		}

		var found []Resource
		for _, w := range workloads(s, kinds...) {
			if w.Replicas < 2 {
				continue
			}
			covered := false
			for _, sel := range budgets[w.Namespace] {
				if sel.Matches(labels.Set(w.PodLabels)) {
					covered = true
					break
				}
			}
			if !covered {
				found = append(found, Resource{
					Namespace: w.Namespace, Kind: w.Kind, Name: w.Name,
					Detail: fmt.Sprintf("%d replicas, no PodDisruptionBudget", w.Replicas),
				})
			}
		}
		return fromResources(found)
	},
}
