package checks

import (
	"strings"

	"github.com/zarakM/shipready/internal/collect"
)

var cost004 = check{
	id: "COST-004",
	meta: Meta{
		Category: Cost,
		Severity: Medium,
		Title:    "No cluster autoscaler or Karpenter detected",
		Description: "Nothing adds or removes nodes as load changes. The cluster stays sized for its busiest hour all day and all night, " +
			"and when it does run out of room, new pods wait until someone adds a node by hand.",
		Remediation: "Install Karpenter (the usual choice on EKS) or the Kubernetes Cluster Autoscaler so node count follows demand.",
	},
	run: func(s *collect.Snapshot) Result {
		// Autoscalers usually live in kube-system or their own namespace.
		if !s.ClusterWide {
			return skipped(needsWholeCluster)
		}
		// Karpenter, including EKS Auto Mode, registers this API group.
		if s.HasAPI(collect.GroupKarpenter) {
			return Result{Status: Pass}
		}
		if reason := s.Unavailable(collect.ResDeployments); reason != "" {
			return skipped(reason)
		}
		for _, d := range s.Deployments {
			if nodeAutoscaler(d.Name) {
				return Result{Status: Pass}
			}
			for _, c := range d.Spec.Template.Spec.Containers {
				if nodeAutoscaler(c.Image) {
					return Result{Status: Pass}
				}
			}
		}
		return fromResources(clusterFinding(s, "no cluster autoscaler or Karpenter found"))
	},
}

func nodeAutoscaler(name string) bool {
	return strings.Contains(name, "cluster-autoscaler") || strings.Contains(name, "karpenter")
}
