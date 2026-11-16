package report

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/zarakM/shipready/internal/checks"
)

var categoryNames = map[checks.Category]string{
	checks.Reliability: "Reliability",
	checks.Security:    "Security",
	checks.Cost:        "Cost",
	checks.Recovery:    "Recovery",
}

// WriteSummary prints the short terminal summary: overall score, a score per
// category and the top findings. reportPath is where the full report went.
func WriteSummary(out io.Writer, r Report, reportPath string) error {
	var b strings.Builder

	fmt.Fprintf(&b, "ShipReady scan of %s", r.Cluster)
	if r.KubernetesVersion != "" {
		fmt.Fprintf(&b, " (Kubernetes %s)", r.KubernetesVersion)
	}
	b.WriteString("\n")
	if len(r.Namespaces) > 0 {
		fmt.Fprintf(&b, "Namespaces: %s\n", strings.Join(r.Namespaces, ", "))
	}
	b.WriteString("\n")

	if r.Score.Overall != nil {
		fmt.Fprintf(&b, "Overall score: %d/100 (grade %s)\n\n", *r.Score.Overall, r.Score.Grade)
	} else {
		b.WriteString("Overall score: not available, because no check could run\n\n")
	}

	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range r.Score.Categories {
		value := "n/a"
		if c.Score != nil {
			value = fmt.Sprintf("%d %s", *c.Score, c.Grade)
		}
		if _, err := fmt.Fprintf(w, "  %s\t%s\t%d passed, %d failed, %d skipped\n",
			categoryNames[c.Category], value, c.Passed, c.Failed, c.Skipped); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	byID := map[string]Check{}
	for _, c := range r.Checks {
		byID[c.ID] = c
	}
	if len(r.TopPriorities) == 0 {
		b.WriteString("\nNo failed checks.\n")
	} else {
		b.WriteString("\nTop priorities\n")
		for i, id := range r.TopPriorities {
			c := byID[id]
			fmt.Fprintf(&b, "  %d. [%s] %s %s (%s)\n", i+1, strings.ToUpper(string(c.Severity)), c.ID, c.Title, affected(len(c.Resources)))
			fmt.Fprintf(&b, "     Fix: %s\n", c.Remediation)
		}
		if more := r.Summary.Failed - len(r.TopPriorities); more > 0 {
			fmt.Fprintf(&b, "  ...and %d more in the report.\n", more)
		}
	}

	if r.Summary.Skipped > 0 {
		fmt.Fprintf(&b, "\n%s not run; the report lists each one with the reason.\n", plural(r.Summary.Skipped, "check was", "checks were"))
	}
	fmt.Fprintf(&b, "\nFull report: %s\n", reportPath)

	_, err := io.WriteString(out, b.String())
	return err
}

func affected(n int) string {
	return plural(n, "resource", "resources") + " affected"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
