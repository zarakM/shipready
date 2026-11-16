package checks

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var sec006 = check{
	id: "SEC-006",
	meta: Meta{
		Category: Security,
		Severity: Medium,
		Title:    "Services of type LoadBalancer or NodePort exposed publicly",
		Description: "Each of these services is reachable from the internet, bypassing whatever ingress, WAF or authentication sits in front of the rest of the platform. " +
			"They are often left over from debugging.",
		Remediation: "Route public traffic through one ingress. Make other load balancers internal, or restrict them to known source IP ranges.",
		Snippet: `metadata:
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-scheme: internal`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResServices, collect.ResNodes); reason != "" {
			return skipped(reason)
		}
		// A NodePort is only public when nodes have public addresses.
		publicNodes := false
		for _, n := range s.Nodes {
			for _, a := range n.Status.Addresses {
				if a.Type == corev1.NodeExternalIP && a.Address != "" {
					publicNodes = true
				}
			}
		}

		var found []Resource
		for i := range s.Services {
			svc := &s.Services[i]
			if !s.InScope(svc.Namespace) {
				continue
			}
			detail := ""
			switch svc.Spec.Type {
			case corev1.ServiceTypeLoadBalancer:
				if publicLoadBalancer(svc) {
					detail = "internet-facing load balancer with no source IP restriction"
				}
			case corev1.ServiceTypeNodePort:
				if publicNodes {
					detail = "NodePort reachable on the nodes' public IP addresses"
				}
			}
			if detail != "" {
				found = append(found, Resource{Namespace: svc.Namespace, Kind: "Service", Name: svc.Name, Detail: detail})
			}
		}
		return fromResources(found)
	},
}

func publicLoadBalancer(svc *corev1.Service) bool {
	ann := svc.Annotations

	// Restricted to specific source ranges, none of which is "everyone".
	if ranges := svc.Spec.LoadBalancerSourceRanges; len(ranges) > 0 {
		open := false
		for _, r := range ranges {
			if r == "0.0.0.0/0" || r == "::/0" {
				open = true
			}
		}
		if !open {
			return false
		}
	}

	const awsScheme = "service.beta.kubernetes.io/aws-load-balancer-scheme"
	switch ann[awsScheme] {
	case "internal":
		return false
	case "internet-facing":
		return true
	}
	// Load balancers built by the AWS Load Balancer Controller are internal
	// unless the scheme says otherwise.
	lbType := ann["service.beta.kubernetes.io/aws-load-balancer-type"]
	class := ""
	if svc.Spec.LoadBalancerClass != nil {
		class = *svc.Spec.LoadBalancerClass
	}
	if class == "service.k8s.aws/nlb" || lbType == "external" || lbType == "nlb-ip" {
		return false
	}

	internalAnnotations := map[string]string{
		"service.beta.kubernetes.io/aws-load-balancer-internal":   "true",
		"service.beta.kubernetes.io/azure-load-balancer-internal": "true",
		"networking.gke.io/load-balancer-type":                    "internal",
		"cloud.google.com/load-balancer-type":                     "internal",
	}
	for key, want := range internalAnnotations {
		if strings.EqualFold(ann[key], want) {
			return false
		}
	}
	return true
}
