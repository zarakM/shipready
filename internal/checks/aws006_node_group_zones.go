package checks

import (
	"github.com/zarakM/shipready/internal/collect"
)

var aws006 = check{
	id: "AWS-006",
	meta: Meta{
		Category: Reliability,
		Severity: Critical,
		Title:    "Node groups in a single AZ",
		Description: "Every subnet of this node group is in one availability zone, so all of its nodes are too. " +
			"When that zone has an outage the group loses every node at once, and it cannot launch replacements anywhere else.",
		Remediation: "Give each node group subnets in at least two availability zones. Managed node group subnets cannot be edited, so create a replacement group and move workloads across.",
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResEKSNodeGroups)
		if a == nil {
			return skipped(reason)
		}
		if len(a.NodeGroups) == 0 {
			return skipped("the cluster has no managed node groups; its nodes come from Karpenter, Auto Mode, Fargate or self-managed groups")
		}
		var found []Resource
		for _, g := range a.NodeGroups {
			if len(g.Zones) == 1 {
				found = append(found, Resource{Kind: "Node group", Name: g.Name, Detail: "all subnets are in " + g.Zones[0]})
			}
		}
		return fromResources(found)
	},
}
