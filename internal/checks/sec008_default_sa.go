package checks

import (
	"github.com/zarakM/shipready/internal/collect"
)

var sec008 = check{
	id: "SEC-008",
	meta: Meta{
		Category: Security,
		Severity: Low,
		Title:    "Workloads using the default ServiceAccount with token automount",
		Description: "Every pod on the default ServiceAccount shares one identity and gets its API token mounted, needed or not. " +
			"Any permission later granted to that account is granted to all of them at once.",
		Remediation: "Give each workload its own ServiceAccount, and turn off token mounting for pods that never call the Kubernetes API.",
		Snippet: `spec:
  serviceAccountName: api
  automountServiceAccountToken: false`,
	},
	run: func(s *collect.Snapshot) Result {
		if reason := s.Unavailable(collect.ResServiceAccounts); reason != "" {
			return skipped(reason)
		}
		// Namespaces whose default ServiceAccount has token mounting off.
		defaultOff := map[string]bool{}
		for _, sa := range s.ServiceAccounts {
			if sa.Name == "default" && sa.AutomountServiceAccountToken != nil && !*sa.AutomountServiceAccountToken {
				defaultOff[sa.Namespace] = true
			}
		}
		return workloadCheck(s, allKinds, func(w workload) string {
			if name := w.Spec.ServiceAccountName; name != "" && name != "default" {
				return ""
			}
			// The pod-level setting wins over the ServiceAccount's.
			if mount := w.Spec.AutomountServiceAccountToken; mount != nil {
				if *mount {
					return "uses the default ServiceAccount with its token mounted"
				}
				return ""
			}
			if defaultOff[w.Namespace] {
				return ""
			}
			return "uses the default ServiceAccount with its token mounted"
		})
	},
}
