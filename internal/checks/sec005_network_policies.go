package checks

import (
	"github.com/zarakM/shipready/internal/collect"
)

var sec005 = check{
	id: "SEC-005",
	meta: Meta{
		Category: Security,
		Severity: Medium,
		Title:    "Namespaces with no NetworkPolicy",
		Description: "With no NetworkPolicy, every pod in the cluster can connect to every pod in this namespace. " +
			"One compromised container can reach your databases and internal APIs directly.",
		Remediation: "Start with a default-deny policy per namespace, then allow the specific traffic each service needs.",
		Snippet: `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-ingress
spec:
  podSelector: {}
  policyTypes: ["Ingress"]`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := unavailable(s, allKinds...); reason != "" {
			return skipped(reason)
		}
		if reason := s.Unavailable(collect.ResNetworkPolicies); reason != "" {
			return skipped(reason)
		}
		protected := map[string]bool{}
		for _, p := range s.NetworkPolicies {
			protected[p.Namespace] = true
		}
		// Only namespaces that actually run something are judged.
		seen := map[string]bool{}
		var found []Resource
		for _, w := range workloads(s, allKinds...) {
			if seen[w.Namespace] || protected[w.Namespace] {
				continue
			}
			seen[w.Namespace] = true
			found = append(found, Resource{Kind: "Namespace", Name: w.Namespace, Detail: "runs workloads with no NetworkPolicy"})
		}
		return fromResources(found)
	},
}
