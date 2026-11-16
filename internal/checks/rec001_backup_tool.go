package checks

import (
	"fmt"
	"strings"

	"github.com/zarakM/shipready/internal/collect"
)

var rec001 = check{
	id: "REC-001",
	meta: Meta{
		Category: Recovery,
		Severity: Critical,
		Title:    "No backup tool detected (Velero or equivalent)",
		Description: "The cluster holds persistent data and nothing in it takes backups. " +
			"A deleted namespace, a bad migration or a lost volume means that data is gone for good. " +
			"ShipReady looks inside the cluster, so backups taken outside it (for example by AWS Backup) are not visible to this check.",
		Remediation: "Install Velero with a daily schedule that includes volume data, send backups to a bucket in another account or region, and rehearse a restore.",
		Snippet: `velero schedule create daily \
  --schedule "0 2 * * *" \
  --ttl 720h`,
	},
	run: func(s *collect.Snapshot) Result {
		if !s.ClusterWide {
			return skipped(needsWholeCluster)
		}
		if backupToolInstalled(s) {
			return Result{Status: Pass}
		}
		if reason := s.Unavailable(collect.ResPVCs); reason != "" {
			return skipped(reason)
		}
		// With no persistent data in the cluster there is nothing here that a
		// backup tool would protect.
		claims := 0
		for _, pvc := range s.PVCs {
			if s.InScope(pvc.Namespace) {
				claims++
			}
		}
		if claims == 0 {
			return Result{Status: Pass}
		}
		unit := "PersistentVolumeClaims"
		if claims == 1 {
			unit = "PersistentVolumeClaim"
		}
		return fromResources(clusterFinding(s, fmt.Sprintf("%d %s and no backup tool found", claims, unit)))
	},
}

// backupToolInstalled looks for the API groups of Velero, Kasten K10, Stash,
// KubeStash and TrilioVault.
func backupToolInstalled(s *collect.Snapshot) bool {
	for group := range s.APIGroups {
		switch {
		case group == collect.GroupVelero,
			strings.HasSuffix(group, "kio.kasten.io"),
			strings.HasSuffix(group, "stash.appscode.com"),
			strings.HasSuffix(group, "kubestash.com"),
			strings.HasSuffix(group, "triliovault.trilio.io"):
			return true
		}
	}
	return false
}
