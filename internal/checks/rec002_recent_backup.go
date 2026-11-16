package checks

import (
	"fmt"
	"time"

	"github.com/zarakM/shipready/internal/collect"
)

const backupMaxAge = 7 * 24 * time.Hour

var rec002 = check{
	id: "REC-002",
	meta: Meta{
		Category: Recovery,
		Severity: High,
		Title:    "Velero installed but no successful backup in the last 7 days",
		Description: "Velero is installed, but it has not finished a backup successfully in over a week. " +
			"Backups that quietly stopped working are worse than none, because everyone believes they are covered.",
		Remediation: "Run `velero backup get` and `velero backup logs <name>` to see why backups fail. Expired cloud credentials and a full or missing bucket are the usual causes. Alert on backup failures.",
	},
	run: func(s *collect.Snapshot) Result {
		if !s.HasAPI(collect.GroupVelero) {
			return skipped("Velero is not installed")
		}
		if reason := s.Unavailable(collect.ResVeleroBackups); reason != "" {
			return skipped(reason)
		}
		var latest time.Time
		for _, b := range s.VeleroBackups {
			if b.Phase == "Completed" && b.Completed.After(latest) {
				latest = b.Completed
			}
		}
		age := s.CollectedAt.Sub(latest)
		switch {
		case latest.IsZero():
			return fromResources(clusterFinding(s, "Velero has never completed a backup"))
		case age > backupMaxAge:
			return fromResources(clusterFinding(s, fmt.Sprintf("last successful Velero backup was %d days ago", int(age.Hours()/24))))
		}
		return Result{Status: Pass}
	},
}
