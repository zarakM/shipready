package checks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

// Registry is the ordered set of checks a scan runs.
type Registry struct {
	checks []Check
}

// Outcome pairs a check with its result.
type Outcome struct {
	Check  Check
	Result Result
}

// NewRegistry builds a registry, rejecting empty and duplicate IDs. Checks are
// ordered by category, then ID.
func NewRegistry(cs ...Check) (*Registry, error) {
	seen := map[string]bool{}
	for _, c := range cs {
		id := c.ID()
		if id == "" {
			return nil, fmt.Errorf("check %q has no ID", c.Meta().Title)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate check ID %s", id)
		}
		seen[id] = true
	}
	order := map[Category]int{}
	for i, c := range Categories {
		order[c] = i
	}
	sorted := append([]Check(nil), cs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := order[sorted[i].Meta().Category], order[sorted[j].Meta().Category]
		if ci != cj {
			return ci < cj
		}
		return sorted[i].ID() < sorted[j].ID()
	})
	return &Registry{checks: sorted}, nil
}

// Default returns the registry of built-in checks.
func Default() *Registry {
	r, err := NewRegistry(
		rel001,
		rel002,
		rel003,
		rel004,
		rel005,
	)
	if err != nil {
		panic(err) // a programming error caught by the registry test
	}
	return r
}

// All returns the checks in report order.
func (r *Registry) All() []Check {
	return append([]Check(nil), r.checks...)
}

// ValidateSkip reports the first --skip value that is neither a check ID nor a
// category.
func (r *Registry) ValidateSkip(skip []string) error {
	known := map[string]bool{}
	for _, c := range Categories {
		known[string(c)] = true
	}
	for _, c := range r.checks {
		known[strings.ToLower(c.ID())] = true
	}
	for _, s := range skip {
		if !known[strings.ToLower(strings.TrimSpace(s))] {
			return fmt.Errorf("unknown check ID or category %q in --skip (run `shipready checks` to list them)", s)
		}
	}
	return nil
}

// Run executes every check against the snapshot. Checks named in skip, by ID
// or category, are reported as skipped instead of being run.
func (r *Registry) Run(s *collect.Snapshot, skip []string) []Outcome {
	skipSet := map[string]bool{}
	for _, v := range skip {
		skipSet[strings.ToLower(strings.TrimSpace(v))] = true
	}
	out := make([]Outcome, 0, len(r.checks))
	for _, c := range r.checks {
		if skipSet[strings.ToLower(c.ID())] || skipSet[string(c.Meta().Category)] {
			out = append(out, Outcome{Check: c, Result: skipped("skipped with --skip")})
			continue
		}
		out = append(out, Outcome{Check: c, Result: c.Run(s)})
	}
	return out
}
