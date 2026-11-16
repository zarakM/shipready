package checks

import (
	"strings"

	"github.com/zarakM/shipready/internal/collect"
)

var sec007 = check{
	id: "SEC-007",
	meta: Meta{
		Category: Security,
		Severity: High,
		Title:    "Pods using hostNetwork, hostPID or hostPath",
		Description: "These settings break the wall between a container and its node. " +
			"hostPath can read the node's files and other pods' data, hostPID can see and signal every process, and hostNetwork bypasses network policies.",
		Remediation: "Remove host access from application workloads. Node agents such as log shippers that truly need it should mount only the specific paths they read, read-only.",
	},
	run: func(s *collect.Snapshot) Result {
		return workloadCheck(s, allKinds, func(w workload) string {
			var uses []string
			if w.Spec.HostNetwork {
				uses = append(uses, "hostNetwork")
			}
			if w.Spec.HostPID {
				uses = append(uses, "hostPID")
			}
			for _, v := range w.Spec.Volumes {
				if v.HostPath != nil {
					uses = append(uses, "hostPath volume "+v.HostPath.Path)
				}
			}
			if len(uses) == 0 {
				return ""
			}
			return "uses " + strings.Join(uses, ", ")
		})
	},
}
