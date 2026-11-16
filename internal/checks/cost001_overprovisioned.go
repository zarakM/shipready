package checks

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/zarakM/shipready/internal/collect"
)

const (
	// A workload is flagged when it uses less than a quarter of a request.
	overRequestFactor = 4
	// Requests smaller than these are not worth anyone's attention.
	minCPURequestMilli = 100
	minMemoryRequest   = 128 << 20
	// Pods younger than this are still warming up.
	minPodAge = 10 * time.Minute
)

var cost001 = check{
	id: "COST-001",
	meta: Meta{
		Category: Cost,
		Severity: Medium,
		Title:    "Requests far above actual usage",
		Description: "Requests reserve node capacity whether or not it is used, and the cluster autoscaler buys nodes to fit them. " +
			"A workload using a fraction of what it reserves is capacity you pay for and nobody can use. " +
			"This compares requests with usage at the moment of the scan, so confirm against a week of metrics before resizing.",
		Remediation: "Lower the requests to just above the workload's real peak usage. A Vertical Pod Autoscaler in recommendation mode will suggest numbers.",
	},
	run: func(s *collect.Snapshot) Result {
		if !s.HasAPI(collect.GroupMetrics) {
			return skipped("metrics-server is not installed, so actual usage is unknown")
		}
		kinds := []string{KindDeployment, KindStatefulSet, KindDaemonSet}
		if reason := unavailable(s, kinds...); reason != "" {
			return skipped(reason)
		}
		if reason := s.Unavailable(collect.ResPods, collect.ResPodMetrics); reason != "" {
			return skipped(reason)
		}

		usage := map[string]collect.PodUsage{}
		for _, u := range s.PodMetrics {
			usage[u.Namespace+"/"+u.Name] = u
		}

		var found []Resource
		for _, w := range workloads(s, kinds...) {
			if w.Selector == nil {
				continue
			}
			selector, err := metav1.LabelSelectorAsSelector(w.Selector)
			if err != nil {
				continue
			}
			var reqCPU, reqMem, useCPU, useMem int64
			measured := false
			for i := range s.Pods {
				p := &s.Pods[i]
				if p.Namespace != w.Namespace || p.Status.Phase != corev1.PodRunning || !selector.Matches(labels.Set(p.Labels)) {
					continue
				}
				if p.Status.StartTime == nil || s.CollectedAt.Sub(p.Status.StartTime.Time) < minPodAge {
					continue
				}
				u, ok := usage[p.Namespace+"/"+p.Name]
				if !ok {
					continue
				}
				measured = true
				for _, c := range p.Spec.Containers {
					reqCPU += c.Resources.Requests.Cpu().MilliValue()
					reqMem += c.Resources.Requests.Memory().Value()
				}
				useCPU += u.CPUMilli
				useMem += u.MemoryBytes
			}
			if !measured {
				continue
			}
			var details []string
			if reqCPU >= minCPURequestMilli && useCPU*overRequestFactor < reqCPU {
				details = append(details, fmt.Sprintf("using %dm of %dm CPU requested (%d%%)", useCPU, reqCPU, useCPU*100/reqCPU))
			}
			if reqMem >= minMemoryRequest && useMem*overRequestFactor < reqMem {
				details = append(details, fmt.Sprintf("using %dMi of %dMi memory requested (%d%%)", useMem>>20, reqMem>>20, useMem*100/reqMem))
			}
			if len(details) > 0 {
				found = append(found, Resource{Namespace: w.Namespace, Kind: w.Kind, Name: w.Name, Detail: strings.Join(details, "; ")})
			}
		}
		return fromResources(found)
	},
}
