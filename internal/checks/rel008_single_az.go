package checks

import (
	"github.com/zarakM/shipready/internal/collect"
)

var rel008 = check{
	id: "REL-008",
	meta: Meta{
		Category: Reliability,
		Severity: Critical,
		Title:    "All nodes in a single availability zone",
		Description: "Every node sits in one availability zone. If that zone has an outage, which cloud providers have several times a year, " +
			"the whole cluster goes down with it and nothing can be rescheduled.",
		Remediation: "Spread node groups across at least two availability zones, three if the region has them, and let pods spread with topology constraints.",
		Snippet: `topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app: api`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResNodes); reason != "" {
			return skipped(reason)
		}
		if len(s.Nodes) == 0 {
			return skipped("no nodes were found")
		}
		zones := map[string]bool{}
		var found []Resource
		for _, n := range s.Nodes {
			zone := n.Labels["topology.kubernetes.io/zone"]
			if zone == "" {
				zone = n.Labels["failure-domain.beta.kubernetes.io/zone"]
			}
			if zone == "" {
				continue
			}
			zones[zone] = true
			found = append(found, Resource{Kind: "Node", Name: n.Name, Detail: "zone " + zone})
		}
		switch len(zones) {
		case 0:
			return skipped("nodes carry no availability zone labels")
		case 1:
			return fromResources(found)
		default:
			return Result{Status: Pass}
		}
	},
}
