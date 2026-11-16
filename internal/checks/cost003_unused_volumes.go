package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var cost003 = check{
	id: "COST-003",
	meta: Meta{
		Category: Cost,
		Severity: Low,
		Title:    "PersistentVolumes in Released or unbound state",
		Description: "These volumes are attached to nothing, but the cloud disks behind them are still billed every month. " +
			"They are usually left behind when a workload is deleted.",
		Remediation: "Check whether the data is still needed. Snapshot anything worth keeping, then delete the volume.",
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResPVs); reason != "" {
			return skipped(reason)
		}
		var found []Resource
		for _, pv := range s.PVs {
			switch pv.Status.Phase {
			case corev1.VolumeReleased, corev1.VolumeAvailable, corev1.VolumeFailed:
				detail := string(pv.Status.Phase)
				if size, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
					detail += ", " + size.String()
				}
				found = append(found, Resource{Kind: "PersistentVolume", Name: pv.Name, Detail: detail})
			}
		}
		return fromResources(found)
	},
}
