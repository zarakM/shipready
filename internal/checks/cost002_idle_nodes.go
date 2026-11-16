package checks

import (
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

// A node is under-used when less than this share of both its CPU and its
// memory is requested.
const lowAllocation = 0.30

var cost002 = check{
	id: "COST-002",
	meta: Meta{
		Category: Cost,
		Severity: Medium,
		Title:    "Nodes with low allocation (under 30% requested)",
		Description: "These nodes are mostly empty: less than 30% of their CPU and memory is reserved by pods. " +
			"You are paying full price for each of them.",
		Remediation: "Let a cluster autoscaler or Karpenter consolidate pods onto fewer nodes, or move to smaller instance types. Check for pod settings that block consolidation.",
	},
	run: func(s *collect.Snapshot) Result {
		// Node usage includes system pods, so a namespace-limited scan
		// cannot work it out.
		if !s.ClusterWide {
			return skipped(needsWholeCluster)
		}
		if reason := s.Unavailable(collect.ResNodes, collect.ResPods); reason != "" {
			return skipped(reason)
		}
		type total struct{ cpu, mem int64 }
		requested := map[string]*total{}
		for i := range s.Pods {
			p := &s.Pods[i]
			if p.Spec.NodeName == "" || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue
			}
			t := requested[p.Spec.NodeName]
			if t == nil {
				t = &total{}
				requested[p.Spec.NodeName] = t
			}
			for _, c := range p.Spec.Containers {
				t.cpu += c.Resources.Requests.Cpu().MilliValue()
				t.mem += c.Resources.Requests.Memory().Value()
			}
		}

		var found []Resource
		for _, n := range s.Nodes {
			cpu := n.Status.Allocatable.Cpu().MilliValue()
			mem := n.Status.Allocatable.Memory().Value()
			if cpu == 0 || mem == 0 {
				continue
			}
			t := requested[n.Name]
			if t == nil {
				t = &total{}
			}
			cpuShare := float64(t.cpu) / float64(cpu)
			memShare := float64(t.mem) / float64(mem)
			if cpuShare < lowAllocation && memShare < lowAllocation {
				found = append(found, Resource{
					Kind: "Node", Name: n.Name,
					Detail: fmt.Sprintf("%.0f%% of CPU and %.0f%% of memory requested", math.Round(cpuShare*100), math.Round(memShare*100)),
				})
			}
		}
		return fromResources(found)
	},
}
