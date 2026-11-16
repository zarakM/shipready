// Package report turns check outcomes into output: the HTML report, the JSON
// report and the terminal summary.
package report

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/score"
)

// SchemaVersion is bumped on any breaking change to the JSON output.
const SchemaVersion = "1"

// TopPriorityCount is how many findings the report leads with.
const TopPriorityCount = 5

// Report is the stable JSON schema. Fields are only ever added within a
// schema version, never renamed or removed.
type Report struct {
	SchemaVersion     string   `json:"schemaVersion"`
	Tool              Tool     `json:"tool"`
	Cluster           string   `json:"cluster"`
	KubernetesVersion string   `json:"kubernetesVersion,omitempty"`
	ScanTime          string   `json:"scanTime"` // RFC 3339, UTC
	Namespaces        []string `json:"namespaces,omitempty"`
	// AWSRegion is set when the scan included AWS checks.
	AWSRegion string      `json:"awsRegion,omitempty"`
	Score     score.Score `json:"score"`
	Summary   Summary     `json:"summary"`
	// TopPriorities holds the IDs of the most severe failed checks, worst
	// first.
	TopPriorities []string `json:"topPriorities"`
	Checks        []Check  `json:"checks"`
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
	Namespaces  []string // the --namespace filter, empty for the whole cluster
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
		Score:             score.Compute(in.Outcomes),
		TopPriorities:     []string{}, // always an array in JSON
		Checks:            make([]Check, 0, len(in.Outcomes)),
	}
	if in.Snapshot.AWS != nil {
		r.AWSRegion = in.Snapshot.AWS.Region
	}
	for _, o := range score.Top(in.Outcomes, TopPriorityCount) {
		r.TopPriorities = append(r.TopPriorities, o.Check.ID())
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

// Failed reports whether any check at or above the given severity failed.
func (r Report) Failed(atLeast checks.Severity) bool {
	for _, c := range r.Checks {
		if c.Status == checks.Fail && score.Weight(c.Severity) >= score.Weight(atLeast) {
			return true
		}
	}
	return false
}

// RenderJSON returns the report as indented JSON.
func RenderJSON(r Report) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	out, err := RenderJSON(r)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
