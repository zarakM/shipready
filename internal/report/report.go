// Package report turns check outcomes into output. Milestone 1 has the JSON
// renderer; the terminal summary and HTML report follow in later milestones.
package report

import (
	"encoding/json"
	"io"
	"time"

	"github.com/muhammadzarakbinkaleem/shipready/internal/checks"
	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

// SchemaVersion is bumped on any breaking change to the JSON output.
const SchemaVersion = "1"

// Report is the stable JSON schema. Fields are only ever added within a
// schema version, never renamed or removed.
type Report struct {
	SchemaVersion     string   `json:"schemaVersion"`
	Tool              Tool     `json:"tool"`
	Cluster           string   `json:"cluster"`
	KubernetesVersion string   `json:"kubernetesVersion,omitempty"`
	ScanTime          string   `json:"scanTime"` // RFC 3339, UTC
	Namespaces        []string `json:"namespaces,omitempty"`
	Summary           Summary  `json:"summary"`
	Checks            []Check  `json:"checks"`
}

// Tool identifies what produced the report.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Summary counts checks by status.
type Summary struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// Check is one check's metadata and outcome.
type Check struct {
	ID          string            `json:"id"`
	Category    checks.Category   `json:"category"`
	Severity    checks.Severity   `json:"severity"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Remediation string            `json:"remediation"`
	Snippet     string            `json:"snippet,omitempty"`
	Status      checks.Status     `json:"status"`
	SkipReason  string            `json:"skipReason,omitempty"`
	Resources   []checks.Resource `json:"resources"`
}

// Input is everything Build needs.
type Input struct {
	ToolVersion string
	ScanTime    time.Time
	Namespaces  []string // the --namespace filter, empty for all non-system
	Snapshot    *collect.Snapshot
	Outcomes    []checks.Outcome
}

// Build assembles the report.
func Build(in Input) Report {
	r := Report{
		SchemaVersion:     SchemaVersion,
		Tool:              Tool{Name: "shipready", Version: in.ToolVersion},
		Cluster:           in.Snapshot.ClusterName,
		KubernetesVersion: in.Snapshot.KubernetesVersion,
		ScanTime:          in.ScanTime.UTC().Format(time.RFC3339),
		Namespaces:        in.Namespaces,
		Checks:            make([]Check, 0, len(in.Outcomes)),
	}
	for _, o := range in.Outcomes {
		m := o.Check.Meta()
		resources := o.Result.Resources
		if resources == nil {
			resources = []checks.Resource{} // always an array in JSON
		}
		r.Checks = append(r.Checks, Check{
			ID:          o.Check.ID(),
			Category:    m.Category,
			Severity:    m.Severity,
			Title:       m.Title,
			Description: m.Description,
			Remediation: m.Remediation,
			Snippet:     m.Snippet,
			Status:      o.Result.Status,
			SkipReason:  o.Result.SkipReason,
			Resources:   resources,
		})
		switch o.Result.Status {
		case checks.Pass:
			r.Summary.Passed++
		case checks.Fail:
			r.Summary.Failed++
		case checks.Skipped:
			r.Summary.Skipped++
		}
	}
	return r
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
