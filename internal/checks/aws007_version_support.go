package checks

import (
	"fmt"

	"github.com/zarakM/shipready/internal/collect"
)

var aws007 = check{
	id: "AWS-007",
	meta: Meta{
		Category: Reliability,
		Severity: High,
		Title:    "EKS version out of standard support",
		Description: "This Kubernetes version has left standard support. AWS charges several times more per hour to keep running it in extended support, " +
			"and when that ends too the cluster is upgraded automatically at a time you do not choose.",
		Remediation: "Plan an upgrade to a version in standard support. Upgrade one minor version at a time, and check for removed APIs before each step.",
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResEKSCluster, collect.ResEKSVersions)
		if a == nil {
			return skipped(reason)
		}
		c := a.Cluster
		ended := !c.StandardSupportEnds.IsZero() && s.CollectedAt.After(c.StandardSupportEnds)
		outOfSupport := c.VersionStatus == collect.VersionExtendedSupport || c.VersionStatus == collect.VersionUnsupported
		if !ended && !outOfSupport {
			return Result{Status: Pass}
		}
		detail := fmt.Sprintf("runs Kubernetes %s, which is out of standard support", c.Version)
		if !c.StandardSupportEnds.IsZero() {
			detail = fmt.Sprintf("runs Kubernetes %s; standard support ended on %s", c.Version, c.StandardSupportEnds.UTC().Format("2 Jan 2006"))
		}
		return fromResources([]Resource{{Kind: "EKS cluster", Name: c.Name, Detail: detail}})
	},
}
