package checks

import (
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var sec004 = check{
	id: "SEC-004",
	meta: Meta{
		Category: Security,
		Severity: High,
		Title:    "Secrets passed as plain environment variable values",
		Description: "A password written straight into a manifest is visible to anyone who can read the Deployment, " +
			"and it is copied into Git history, CI logs and every `kubectl describe` output.",
		Remediation: "Move the value into a Secret (or an external secret manager) and reference it. Then rotate the credential, since the old value has already been exposed.",
		Snippet: `env:
  - name: DB_PASSWORD
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: password`,
	},
	run: func(s *collect.Snapshot) Result {
		return checkContainers(s, allKinds, true, func(_ workload, c corev1.Container) string {
			var names []string
			for _, e := range c.Env {
				if plainSecret(e) {
					names = append(names, e.Name)
				}
			}
			if len(names) == 0 {
				return ""
			}
			sort.Strings(names)
			// Only the variable names are reported. The values are never
			// read into the result.
			return "plain value set for " + strings.Join(names, ", ")
		})
	},
}

var (
	secretEnvName = regexp.MustCompile(`(?i)(PASSWORD|PASSWD|SECRET|TOKEN|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIAL)`)
	// Names that mention a secret but hold a pointer to one, not the secret.
	secretPointerName = regexp.MustCompile(`(?i)_(FILE|PATH|DIR|NAME|URL|URI|ENDPOINT)$`)
)

// plainSecret decides from the variable's name alone, plus whether a literal
// value is present. It never inspects what the value is beyond spotting a
// $(VAR) reference to another variable.
func plainSecret(e corev1.EnvVar) bool {
	if e.Value == "" || e.ValueFrom != nil || strings.Contains(e.Value, "$(") {
		return false
	}
	return secretEnvName.MatchString(e.Name) && !secretPointerName.MatchString(e.Name)
}
