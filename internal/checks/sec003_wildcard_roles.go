package checks

import (
	"slices"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var sec003 = check{
	id: "SEC-003",
	meta: Meta{
		Category: Security,
		Severity: High,
		Title:    "Roles with wildcard verbs or resources",
		Description: "A role with `*` grants more than anyone intended, including access to resource types added later. " +
			"Wildcard resources almost always include Secrets.",
		Remediation: "List the exact resources and verbs the role needs. `kubectl auth can-i --list` shows what a subject can currently do.",
		Snippet: `rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch"]`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResRoles, collect.ResClusterRoles); reason != "" {
			return skipped(reason)
		}
		var found []Resource
		for _, r := range s.ClusterRoles {
			if builtInRole(r.Name, r.Labels) {
				continue
			}
			if detail := wildcardDetail(r.Rules); detail != "" {
				found = append(found, Resource{Kind: "ClusterRole", Name: r.Name, Detail: detail})
			}
		}
		for _, r := range s.Roles {
			if !s.InScope(r.Namespace) || builtInRole(r.Name, r.Labels) {
				continue
			}
			if detail := wildcardDetail(r.Rules); detail != "" {
				found = append(found, Resource{Namespace: r.Namespace, Kind: "Role", Name: r.Name, Detail: detail})
			}
		}
		return fromResources(found)
	},
}

// builtInRole reports whether a role ships with Kubernetes or EKS rather than
// being something the team wrote.
func builtInRole(name string, labels map[string]string) bool {
	return labels["kubernetes.io/bootstrapping"] == "rbac-defaults" ||
		strings.HasPrefix(name, "system:") || strings.HasPrefix(name, "eks:")
}

func wildcardDetail(rules []rbacv1.PolicyRule) string {
	var verbs, resources bool
	for _, rule := range rules {
		verbs = verbs || slices.Contains(rule.Verbs, "*")
		resources = resources || slices.Contains(rule.Resources, "*")
	}
	switch {
	case verbs && resources:
		return "wildcard verbs and resources"
	case verbs:
		return "wildcard verbs"
	case resources:
		return "wildcard resources"
	}
	return ""
}
