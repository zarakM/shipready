package checks

import (
	"testing"

	"github.com/zarakM/shipready/internal/collect"
)

const (
	costGood = "cost/good.yaml"
	costBad  = "cost/bad.yaml"
)

func TestCOST001OverProvisioned(t *testing.T) {
	runCases(t, cost001, []checkCase{
		{name: "requests close to usage", snap: fixture(t, costGood), status: Pass},
		{name: "over-provisioned; small and young workloads ignored", snap: fixture(t, costBad), status: Fail, want: []string{
			"shop/Deployment/api: using 150m of 3000m CPU requested (5%)",
		}},
		{name: "skipped without metrics-server", snap: fixture(t, "rel/good.yaml"), status: Skipped, skipReason: "metrics-server"},
		{name: "skipped when metrics cannot be read", snap: deny(fixture(t, costBad), collect.ResPodMetrics), status: Skipped, skipReason: "pod metrics"},
		{name: "skipped when pods cannot be listed", snap: deny(fixture(t, costBad), collect.ResPods), status: Skipped, skipReason: "pods"},
		{
			name: "memory is judged the same way",
			snap: with(fixture(t, costBad), func(s *collect.Snapshot) {
				for i := range s.PodMetrics {
					if s.PodMetrics[i].Name == "api-1" {
						s.PodMetrics[i].CPUMilli = 2000
						s.PodMetrics[i].MemoryBytes = 200 << 20
					}
				}
			}),
			status: Fail,
			want:   []string{"shop/Deployment/api: using 200Mi of 2048Mi memory requested (9%)"},
		},
	})
}

func TestCOST002IdleNodes(t *testing.T) {
	runCases(t, cost002, []checkCase{
		{name: "nodes in use", snap: fixture(t, costGood), status: Pass},
		{name: "mostly empty node; finished pods do not count", snap: fixture(t, costBad), status: Fail, want: []string{
			"/Node/big-idle: 6% of CPU and 4% of memory requested",
		}},
		{name: "skipped on a namespace-limited scan", snap: namespaceLimited(fixture(t, costBad)), status: Skipped, skipReason: "without --namespace"},
		{name: "skipped when nodes cannot be listed", snap: deny(fixture(t, costBad), collect.ResNodes), status: Skipped, skipReason: "nodes"},
	})
}

func TestCOST003UnusedVolumes(t *testing.T) {
	runCases(t, cost003, []checkCase{
		{name: "all volumes bound", snap: fixture(t, costGood), status: Pass},
		{name: "released and available volumes", snap: fixture(t, costBad), status: Fail, want: []string{
			"/PersistentVolume/pv-available: Available, 20Gi",
			"/PersistentVolume/pv-released: Released, 100Gi",
		}},
		{name: "skipped when volumes cannot be listed", snap: deny(fixture(t, costBad), collect.ResPVs), status: Skipped, skipReason: "persistentvolumes"},
	})
}

func TestCOST004NodeAutoscaling(t *testing.T) {
	runCases(t, cost004, []checkCase{
		{name: "cluster autoscaler in kube-system", snap: fixture(t, costGood), status: Pass},
		{name: "nothing scales nodes", snap: fixture(t, costBad), status: Fail, want: []string{
			"/Cluster/cluster: no cluster autoscaler or Karpenter found",
		}},
		{
			name: "Karpenter API present",
			snap: with(fixture(t, costBad), func(s *collect.Snapshot) {
				s.APIGroups[collect.GroupKarpenter] = true
			}),
			status: Pass,
		},
		{
			name: "cluster name is used when known",
			snap: with(fixture(t, costBad), func(s *collect.Snapshot) {
				s.ClusterName = "prod-eks"
			}),
			status: Fail,
			want:   []string{"/Cluster/prod-eks: no cluster autoscaler or Karpenter found"},
		},
		{name: "skipped on a namespace-limited scan", snap: namespaceLimited(fixture(t, costBad)), status: Skipped, skipReason: "without --namespace"},
		{name: "skipped when deployments cannot be listed", snap: deny(fixture(t, costBad), collect.ResDeployments), status: Skipped, skipReason: "deployments"},
	})
}
