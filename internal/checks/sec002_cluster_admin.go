package checks

import (
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var sec002 = check{
	id: "SEC-002",
	meta: Meta{
		Category: Security,
		Severity: Critical,
		Title:    "ClusterRoleBindings to `cluster-admin` beyond system subjects",
		Description: "cluster-admin can read every secret and delete every workload in the cluster. " +
			"Each person or service account holding it is a single stolen credential away from a full compromise.",
		Remediation: "Replace cluster-admin with a role limited to what each person or pipeline needs. Keep one break-glass admin identity and audit its use.",
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResClusterRoleBindings); reason != "" {
			return skipped(reason)
		}
		var found []Resource
		for _, b := range s.ClusterRoleBindings {
			if b.RoleRef.Kind != "ClusterRole" || b.RoleRef.Name != "cluster-admin" {
				continue
			}
			var subjects []string
			for _, sub := range b.Subjects {
				if !systemAdminSubject(sub) {
					subjects = append(subjects, describeSubject(sub))
				}
			}
			if len(subjects) > 0 {
				found = append(found, Resource{
					Kind: "ClusterRoleBinding", Name: b.Name,
					Detail: "grants cluster-admin to " + strings.Join(subjects, ", "),
				})
			}
		}
		return fromResources(found)
	},
}

// systemAdminSubject reports whether a cluster-admin subject is part of the
// platform itself. Catch-all groups such as system:authenticated are not: a
// binding to one of those hands admin to everybody.
func systemAdminSubject(sub rbacv1.Subject) bool {
	switch sub.Kind {
	case rbacv1.GroupKind:
		return sub.Name == "system:masters"
	case rbacv1.UserKind:
		if sub.Name == "system:anonymous" {
			return false
		}
		// Control-plane components, and identities EKS manages itself.
		return strings.HasPrefix(sub.Name, "system:") || strings.HasPrefix(sub.Name, "eks:")
	case rbacv1.ServiceAccountKind:
		return sub.Namespace == "kube-system"
	}
	return false
}

func describeSubject(sub rbacv1.Subject) string {
	if sub.Kind == rbacv1.ServiceAccountKind {
		return sub.Kind + " " + sub.Namespace + "/" + sub.Name
	}
	return sub.Kind + " " + sub.Name
}
