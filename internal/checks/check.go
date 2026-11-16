// Package checks holds every production-readiness check. Each check reads the
// collected Snapshot and never calls the cluster.
package checks

import (
	"sort"

	"github.com/zarakM/shipready/internal/collect"
)

// Severity of a check.
type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
	Low      Severity = "low"
)

// Category groups checks in the report and the score.
type Category string

const (
	Reliability Category = "reliability"
	Security    Category = "security"
	Cost        Category = "cost"
	Recovery    Category = "recovery"
)

// Categories lists every category in report order.
var Categories = []Category{Reliability, Security, Cost, Recovery}

// Status is the outcome of one check.
type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Skipped Status = "skipped"
)

// Meta is the static description of a check, written for a busy CTO.
type Meta struct {
	Category    Category
	Severity    Severity
	Title       string
	Description string // why it matters
	Remediation string // how to fix it
	Snippet     string // optional example manifest
}

// Resource is one thing a failed check points at.
type Resource struct {
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Result is what a check returns.
type Result struct {
	Status     Status
	Resources  []Resource // affected resources when Status is Fail
	SkipReason string     // set when Status is Skipped
}

// Check is the interface every check implements.
type Check interface {
	ID() string
	Meta() Meta
	Run(*collect.Snapshot) Result
}

// check is the concrete type the built-in checks use.
type check struct {
	id   string
	meta Meta
	run  func(*collect.Snapshot) Result
}

func (c check) ID() string                     { return c.id }
func (c check) Meta() Meta                     { return c.meta }
func (c check) Run(s *collect.Snapshot) Result { return c.run(s) }

func skipped(reason string) Result {
	return Result{Status: Skipped, SkipReason: reason}
}

// fromResources passes when nothing is affected and fails otherwise, with the
// affected resources in a stable order.
func fromResources(resources []Resource) Result {
	if len(resources) == 0 {
		return Result{Status: Pass}
	}
	sort.SliceStable(resources, func(i, j int) bool {
		a, b := resources[i], resources[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Container < b.Container
	})
	return Result{Status: Fail, Resources: resources}
}
