package checks

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/zarakM/shipready/internal/collect"
)

var rec003 = check{
	id: "REC-003",
	meta: Meta{
		Category: Recovery,
		Severity: High,
		Title:    "StatefulSets with no VolumeSnapshot or backup schedule covering them",
		Description: "These StatefulSets store data on volumes that no backup schedule includes and no recent snapshot covers. " +
			"If the volume is lost or the data is corrupted, there is nothing to restore from.",
		Remediation: "Include the namespace in a Velero schedule with volume backups, or take scheduled VolumeSnapshots of each data volume.",
		Snippet: `apiVersion: velero.io/v1
kind: Schedule
metadata:
  name: daily
  namespace: velero
spec:
  schedule: "0 2 * * *"
  template:
    includedNamespaces: ["*"]
    snapshotVolumes: true`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResStatefulSets); reason != "" {
			return skipped(reason)
		}
		// An installed tool whose data could not be read might be covering
		// everything, so the check cannot judge.
		if s.HasAPI(collect.GroupVelero) {
			if reason := s.Unavailable(collect.ResVeleroSchedules); reason != "" {
				return skipped(reason)
			}
		}
		if s.HasAPI(collect.GroupSnapshots) {
			if reason := s.Unavailable(collect.ResVolumeSnapshots); reason != "" {
				return skipped(reason)
			}
		}

		// Claims with a ready snapshot taken in the last seven days.
		snapshotted := map[string]bool{}
		for _, snap := range s.VolumeSnapshots {
			if snap.Ready && s.CollectedAt.Sub(snap.Created) <= backupMaxAge {
				snapshotted[snap.Namespace+"/"+snap.PVCName] = true
			}
		}

		var found []Resource
		for i := range s.StatefulSets {
			sts := &s.StatefulSets[i]
			if !s.InScope(sts.Namespace) || len(sts.Spec.VolumeClaimTemplates) == 0 {
				continue
			}
			podLabels := labels.Set(sts.Spec.Template.Labels)
			covered := false
			for _, sched := range s.VeleroSchedules {
				if scheduleCovers(sched, sts.Namespace, podLabels) {
					covered = true
					break
				}
			}
			if !covered {
				covered = allVolumesSnapshotted(sts.Namespace, sts.Name, claimTemplateNames(sts.Spec.VolumeClaimTemplates), replicas(sts.Spec.Replicas), snapshotted)
			}
			if !covered {
				found = append(found, Resource{
					Namespace: sts.Namespace, Kind: KindStatefulSet, Name: sts.Name,
					Detail: "no backup schedule or recent volume snapshot covers its data",
				})
			}
		}
		return fromResources(found)
	},
}

// scheduleCovers reports whether an active Velero schedule backs up pods with
// the given labels in the namespace.
func scheduleCovers(sched collect.VeleroSchedule, namespace string, podLabels labels.Set) bool {
	if sched.Paused || slices.Contains(sched.ExcludedNamespaces, namespace) {
		return false
	}
	// An empty include list means every namespace.
	if inc := sched.IncludedNamespaces; len(inc) > 0 && !slices.Contains(inc, "*") && !slices.Contains(inc, namespace) {
		return false
	}
	if sched.LabelSelector == nil {
		return true
	}
	selector, err := metav1.LabelSelectorAsSelector(sched.LabelSelector)
	return err == nil && selector.Matches(podLabels)
}

func claimTemplateNames(templates []corev1.PersistentVolumeClaim) []string {
	names := make([]string, 0, len(templates))
	for _, t := range templates {
		names = append(names, t.Name)
	}
	return names
}

// allVolumesSnapshotted reports whether every data volume of a StatefulSet has
// a recent snapshot. StatefulSet claims are named <template>-<name>-<ordinal>.
func allVolumesSnapshotted(namespace, name string, templates []string, replicaCount int32, snapshotted map[string]bool) bool {
	if replicaCount < 1 {
		replicaCount = 1 // a scaled-down set still has its first volume
	}
	for _, t := range templates {
		for i := int32(0); i < replicaCount; i++ {
			if !snapshotted[fmt.Sprintf("%s/%s-%s-%d", namespace, t, name, i)] {
				return false
			}
		}
	}
	return true
}
