package checks

import (
	"slices"

	"github.com/zarakM/shipready/internal/collect"
)

var aws001 = check{
	id: "AWS-001",
	meta: Meta{
		Category: Security,
		Severity: High,
		Title:    "EKS API endpoint public with no CIDR restriction",
		Description: "Anyone on the internet can reach your cluster's API server. Authentication still stands in the way, " +
			"but one leaked credential or one Kubernetes vulnerability is then enough, with no network barrier at all.",
		Remediation: "Restrict the public endpoint to your office and VPN address ranges, or turn it off and reach the API through the private endpoint.",
		Snippet: `aws eks update-cluster-config --name <cluster> \
  --resources-vpc-config endpointPublicAccess=true,publicAccessCidrs=203.0.113.0/24,endpointPrivateAccess=true`,
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResEKSCluster)
		if a == nil {
			return skipped(reason)
		}
		c := a.Cluster
		// EKS treats an empty list as open to everyone.
		open := len(c.PublicCIDRs) == 0 || slices.Contains(c.PublicCIDRs, "0.0.0.0/0") || slices.Contains(c.PublicCIDRs, "::/0")
		if !c.EndpointPublic || !open {
			return Result{Status: Pass}
		}
		return fromResources([]Resource{{Kind: "EKS cluster", Name: c.Name, Detail: "API endpoint accepts connections from any IP address"}})
	},
}
