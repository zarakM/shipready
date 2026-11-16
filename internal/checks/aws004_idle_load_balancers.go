package checks

import (
	"github.com/zarakM/shipready/internal/collect"
)

var aws004 = check{
	id: "AWS-004",
	meta: Meta{
		Category: Cost,
		Severity: Low,
		Title:    "Load balancers with no healthy targets",
		Description: "A load balancer with nothing healthy behind it serves no traffic but is billed by the hour. " +
			"It is either left over from a service that was removed, or it fronts a service that is down right now.",
		Remediation: "If the service is gone, delete the load balancer (delete the Kubernetes Service or Ingress that created it, so it is not recreated). If the service should be up, look at why its targets fail their health checks.",
	},
	run: func(s *collect.Snapshot) Result {
		a, reason := awsData(s, collect.ResLoadBalancers)
		if a == nil {
			return skipped(reason)
		}
		var found []Resource
		for _, lb := range a.LoadBalancers {
			detail := ""
			switch {
			case lb.TargetGroups == 0:
				detail = "no target groups"
			case lb.RegisteredTargets == 0:
				detail = "no targets registered"
			case lb.HealthyTargets == 0:
				detail = plural(lb.RegisteredTargets, "target", "targets") + " registered, none healthy"
			default:
				continue
			}
			found = append(found, Resource{Kind: loadBalancerKind(lb.Type), Name: lb.Name, Detail: detail})
		}
		return fromResources(found)
	},
}

func loadBalancerKind(lbType string) string {
	switch lbType {
	case "application":
		return "Application Load Balancer"
	case "network":
		return "Network Load Balancer"
	}
	return "Load balancer"
}
