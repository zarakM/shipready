package checks

import (
	"slices"
	"strings"

	"github.com/zarakM/shipready/internal/collect"
)

// The log types needed to reconstruct who did what to the cluster.
var requiredLogTypes = []string{"api", "audit", "authenticator"}

var aws002 = check{
	id: "AWS-002",
	meta: Meta{
		Category: Security,
		Severity: Medium,
		Title:    "EKS control plane logging disabled",
		Description: "Without control plane logs there is no record of who called the Kubernetes API or what they changed. " +
			"After an incident you cannot tell what happened, and the logs cannot be recovered afterwards.",
		Remediation: "Turn on the api, audit and authenticator log types. They go to CloudWatch Logs; set a retention period so the cost stays predictable.",
		Snippet: `aws eks update-cluster-config --name <cluster> \
  --logging '{"clusterLogging":[{"types":["api","audit","authenticator"],"enabled":true}]}'`,
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResEKSCluster)
		if a == nil {
			return skipped(reason)
		}
		var missing []string
		for _, t := range requiredLogTypes {
			if !slices.Contains(a.Cluster.EnabledLogs, t) {
				missing = append(missing, t)
			}
		}
		if len(missing) == 0 {
			return Result{Status: Pass}
		}
		return fromResources([]Resource{{Kind: "EKS cluster", Name: a.Cluster.Name, Detail: "not logging: " + strings.Join(missing, ", ")}})
	},
}
