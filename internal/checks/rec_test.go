package checks

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zarakM/shipready/internal/collect"
)

const (
	recGood = "rec/good.yaml"
	recBad  = "rec/bad.yaml"
)

func claim(namespace, name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func TestREC001BackupTool(t *testing.T) {
	runCases(t, rec001, []checkCase{
		{name: "Velero installed", snap: fixture(t, recGood), status: Pass},
		{
			name:   "persistent data and no backup tool",
			snap:   snapshot(t, claim("shop", "data-db-0"), claim("shop", "data-db-1")),
			status: Fail,
			want:   []string{"/Cluster/cluster: 2 PersistentVolumeClaims and no backup tool found"},
		},
		{name: "no persistent data means nothing to back up", snap: snapshot(t), status: Pass},
		{name: "claims in system namespaces do not count", snap: snapshot(t, claim("kube-system", "prometheus")), status: Pass},
		{
			name: "another backup tool counts",
			snap: with(snapshot(t, claim("shop", "data-db-0")), func(s *collect.Snapshot) {
				s.APIGroups["config.kio.kasten.io"] = true
			}),
			status: Pass,
		},
		{name: "skipped on a namespace-limited scan", snap: namespaceLimited(snapshot(t, claim("shop", "data-db-0"))), status: Skipped, skipReason: "without --namespace"},
		{name: "skipped when claims cannot be listed", snap: deny(snapshot(t), collect.ResPVCs), status: Skipped, skipReason: "persistentvolumeclaims"},
	})
}

func TestREC002RecentBackup(t *testing.T) {
	runCases(t, rec002, []checkCase{
		{name: "backup completed this morning", snap: fixture(t, recGood), status: Pass},
		{name: "last success two weeks ago; a recent failure does not count", snap: fixture(t, recBad), status: Fail, want: []string{
			"/Cluster/cluster: last successful Velero backup was 15 days ago",
		}},
		{
			name: "no backup has ever completed",
			snap: with(fixture(t, recBad), func(s *collect.Snapshot) {
				s.VeleroBackups = nil
			}),
			status: Fail,
			want:   []string{"/Cluster/cluster: Velero has never completed a backup"},
		},
		{name: "skipped without Velero", snap: snapshot(t), status: Skipped, skipReason: "not installed"},
		{name: "skipped when backups cannot be listed", snap: deny(fixture(t, recBad), collect.ResVeleroBackups), status: Skipped, skipReason: "Velero backups"},
	})
}

func TestREC003StatefulSetCoverage(t *testing.T) {
	runCases(t, rec003, []checkCase{
		{name: "schedule covers every namespace", snap: fixture(t, recGood), status: Pass},
		{name: "one set uncovered; snapshots and volume-less sets are fine", snap: fixture(t, recBad), status: Fail, want: []string{
			"shop/StatefulSet/db: no backup schedule or recent volume snapshot covers its data",
		}},
		{
			name: "an excluded namespace is not covered",
			snap: with(fixture(t, recGood), func(s *collect.Snapshot) {
				s.VeleroSchedules[0].ExcludedNamespaces = []string{"shop"}
			}),
			status: Fail,
			want:   []string{"shop/StatefulSet/db: no backup schedule or recent volume snapshot covers its data"},
		},
		{
			name: "a schedule whose label selector matches the pods covers them",
			snap: with(fixture(t, recBad), func(s *collect.Snapshot) {
				for i := range s.VeleroSchedules {
					if s.VeleroSchedules[i].Name == "other-app" {
						s.VeleroSchedules[i].LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}}
					}
				}
			}),
			status: Pass,
		},
		{
			name: "with no backup tooling at all, sets with volumes fail",
			snap: with(fixture(t, recBad), func(s *collect.Snapshot) {
				s.APIGroups = map[string]bool{}
				s.VeleroSchedules, s.VolumeSnapshots = nil, nil
			}),
			status: Fail,
			want: []string{
				"shop/StatefulSet/db: no backup schedule or recent volume snapshot covers its data",
				"shop/StatefulSet/kv: no backup schedule or recent volume snapshot covers its data",
			},
		},
		{name: "skipped when schedules cannot be listed", snap: deny(fixture(t, recBad), collect.ResVeleroSchedules), status: Skipped, skipReason: "Velero schedules"},
		{name: "skipped when snapshots cannot be listed", snap: deny(fixture(t, recBad), collect.ResVolumeSnapshots), status: Skipped, skipReason: "volume snapshots"},
		{name: "skipped when statefulsets cannot be listed", snap: deny(fixture(t, recBad), collect.ResStatefulSets), status: Skipped, skipReason: "statefulsets"},
	})
}
