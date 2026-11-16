package checks

import (
	"fmt"

	"github.com/zarakM/shipready/internal/collect"
)

const minBackupRetentionDays = 7

var aws005 = check{
	id: "AWS-005",
	meta: Meta{
		Category: Recovery,
		Severity: Critical,
		Title:    "RDS instances without automated backups or with retention under 7 days",
		Description: "The database is where the data you cannot recreate lives. With backups off, a dropped table or a bad migration is permanent. " +
			"With only a few days kept, a problem noticed after the weekend may already be too old to undo.",
		Remediation: "Set the backup retention period to at least 7 days on every production database. Point-in-time recovery comes with it.",
		Snippet: `aws rds modify-db-instance --db-instance-identifier <id> \
  --backup-retention-period 7 --apply-immediately`,
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResRDS)
		if a == nil {
			return skipped(reason)
		}
		var found []Resource
		for _, db := range a.Databases {
			if db.RetentionDays >= minBackupRetentionDays {
				continue
			}
			kind := "RDS instance"
			if db.Cluster {
				kind = "RDS cluster"
			}
			detail := "automated backups are off"
			if db.RetentionDays > 0 {
				detail = "backups are kept for only " + plural(int(db.RetentionDays), "day", "days")
			}
			found = append(found, Resource{Kind: kind, Name: db.ID, Detail: fmt.Sprintf("%s (%s)", detail, db.Engine)})
		}
		return fromResources(found)
	},
}
