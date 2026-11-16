package checks

import (
	"fmt"

	"github.com/zarakM/shipready/internal/collect"
)

var aws003 = check{
	id: "AWS-003",
	meta: Meta{
		Category: Cost,
		Severity: Low,
		Title:    "Unattached EBS volumes",
		Description: "These disks are attached to nothing and are billed every month all the same. " +
			"They are usually left behind when an instance or a Kubernetes volume is deleted.",
		Remediation: "Check whether the data is still needed. Snapshot anything worth keeping, which costs far less than the volume, then delete the volume.",
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResEBSVolumes)
		if a == nil {
			return skipped(reason)
		}
		var found []Resource
		for _, v := range a.Volumes {
			detail := fmt.Sprintf("%d GiB %s, attached to nothing", v.SizeGiB, v.Type)
			if v.Name != "" {
				detail += fmt.Sprintf(" (named %q)", v.Name)
			}
			found = append(found, Resource{Kind: "EBS volume", Name: v.ID, Detail: detail})
		}
		return fromResources(found)
	},
}
