package score

import (
	"strconv"
	"testing"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
)

// stub is a check with a fixed result.
type stub struct {
	id       string
	category checks.Category
	severity checks.Severity
}

func (s stub) ID() string                          { return s.id }
func (s stub) Meta() checks.Meta                   { return checks.Meta{Category: s.category, Severity: s.severity} }
func (s stub) Run(*collect.Snapshot) checks.Result { return checks.Result{} }

func outcome(id string, cat checks.Category, sev checks.Severity, status checks.Status, affected int) checks.Outcome {
	return checks.Outcome{
		Check:  stub{id, cat, sev},
		Result: checks.Result{Status: status, Resources: make([]checks.Resource, affected)},
	}
}

func intPtr(v int) *int { return &v }

func TestCompute(t *testing.T) {
	const (
		rel = checks.Reliability
		sec = checks.Security
		cst = checks.Cost
		rec = checks.Recovery
	)
	cases := []struct {
		name       string
		outcomes   []checks.Outcome
		categories map[checks.Category]*int
		overall    *int
		grade      string
	}{
		{
			name: "everything passes",
			outcomes: []checks.Outcome{
				outcome("REL-1", rel, checks.High, checks.Pass, 0),
				outcome("SEC-1", sec, checks.Critical, checks.Pass, 0),
				outcome("COST-1", cst, checks.Low, checks.Pass, 0),
				outcome("REC-1", rec, checks.Medium, checks.Pass, 0),
			},
			categories: map[checks.Category]*int{rel: intPtr(100), sec: intPtr(100), cst: intPtr(100), rec: intPtr(100)},
			overall:    intPtr(100), grade: "A",
		},
		{
			// Reliability: failed 10 of 17 -> 41. Security: failed 5 of 6 -> 17.
			// Cost: 100. Recovery: 0. Overall: (41+17+100+0)/4 = 39.5 -> 40.
			name: "weights by severity",
			outcomes: []checks.Outcome{
				outcome("REL-1", rel, checks.Critical, checks.Fail, 1),
				outcome("REL-2", rel, checks.High, checks.Pass, 0),
				outcome("REL-3", rel, checks.Medium, checks.Pass, 0),
				outcome("SEC-1", sec, checks.High, checks.Fail, 1),
				outcome("SEC-2", sec, checks.Low, checks.Pass, 0),
				outcome("COST-1", cst, checks.Medium, checks.Pass, 0),
				outcome("REC-1", rec, checks.Critical, checks.Fail, 1),
			},
			categories: map[checks.Category]*int{rel: intPtr(41), sec: intPtr(17), cst: intPtr(100), rec: intPtr(0)},
			overall:    intPtr(40), grade: "D",
		},
		{
			name: "a check counts once however many resources it affects",
			outcomes: []checks.Outcome{
				outcome("REL-1", rel, checks.High, checks.Fail, 250),
				outcome("REL-2", rel, checks.High, checks.Pass, 0),
			},
			categories: map[checks.Category]*int{rel: intPtr(50), sec: nil, cst: nil, rec: nil},
			overall:    intPtr(50), grade: "D",
		},
		{
			// Skipped checks drop out of the weight entirely, and a category
			// with nothing left is not averaged in.
			name: "skipped checks and unscored categories",
			outcomes: []checks.Outcome{
				outcome("REL-1", rel, checks.Critical, checks.Skipped, 0),
				outcome("REL-2", rel, checks.High, checks.Pass, 0),
				outcome("SEC-1", sec, checks.High, checks.Fail, 1),
				outcome("SEC-2", sec, checks.High, checks.Pass, 0),
				outcome("REC-1", rec, checks.Critical, checks.Skipped, 0),
			},
			categories: map[checks.Category]*int{rel: intPtr(100), sec: intPtr(50), cst: nil, rec: nil},
			overall:    intPtr(75), grade: "B",
		},
		{
			name:       "nothing ran",
			outcomes:   []checks.Outcome{outcome("REL-1", rel, checks.High, checks.Skipped, 0)},
			categories: map[checks.Category]*int{rel: nil, sec: nil, cst: nil, rec: nil},
			overall:    nil, grade: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(tc.outcomes)
			if !same(got.Overall, tc.overall) || got.Grade != tc.grade {
				t.Errorf("overall = %s %q, want %s %q", show(got.Overall), got.Grade, show(tc.overall), tc.grade)
			}
			if len(got.Categories) != len(checks.Categories) {
				t.Fatalf("categories = %d, want all %d", len(got.Categories), len(checks.Categories))
			}
			for i, c := range got.Categories {
				if c.Category != checks.Categories[i] {
					t.Errorf("category %d = %s, want report order", i, c.Category)
				}
				if want := tc.categories[c.Category]; !same(c.Score, want) {
					t.Errorf("%s = %s, want %s", c.Category, show(c.Score), show(want))
				}
				if (c.Score == nil) != (c.Grade == "") {
					t.Errorf("%s: grade %q does not match score %s", c.Category, c.Grade, show(c.Score))
				}
			}
		})
	}
}

func TestCategoryCounts(t *testing.T) {
	got := Compute([]checks.Outcome{
		outcome("REL-1", checks.Reliability, checks.High, checks.Pass, 0),
		outcome("REL-2", checks.Reliability, checks.High, checks.Fail, 2),
		outcome("REL-3", checks.Reliability, checks.High, checks.Fail, 1),
		outcome("REL-4", checks.Reliability, checks.High, checks.Skipped, 0),
	}).Categories[0]
	if got.Passed != 1 || got.Failed != 2 || got.Skipped != 1 {
		t.Errorf("counts = %d passed, %d failed, %d skipped; want 1, 2, 1", got.Passed, got.Failed, got.Skipped)
	}
}

func TestGradeBands(t *testing.T) {
	cases := map[int]string{100: "A", 90: "A", 89: "B", 75: "B", 74: "C", 60: "C", 59: "D", 40: "D", 39: "F", 0: "F"}
	for score, want := range cases {
		if got := Grade(score); got != want {
			t.Errorf("Grade(%d) = %s, want %s", score, got, want)
		}
	}
}

func TestTop(t *testing.T) {
	outcomes := []checks.Outcome{
		outcome("REL-002", checks.Reliability, checks.High, checks.Fail, 3),
		outcome("REL-001", checks.Reliability, checks.High, checks.Fail, 3),
		outcome("SEC-005", checks.Security, checks.Medium, checks.Fail, 40),
		outcome("REC-001", checks.Recovery, checks.Critical, checks.Fail, 1),
		outcome("SEC-001", checks.Security, checks.High, checks.Fail, 9),
		outcome("SEC-002", checks.Security, checks.Critical, checks.Pass, 0),
		outcome("COST-003", checks.Cost, checks.Low, checks.Fail, 2),
		outcome("REL-008", checks.Reliability, checks.Critical, checks.Skipped, 0),
	}
	want := []string{"REC-001", "SEC-001", "REL-001", "REL-002", "SEC-005"}
	got := Top(outcomes, 5)
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d", len(got), len(want))
	}
	for i, o := range got {
		if o.Check.ID() != want[i] {
			t.Errorf("position %d = %s, want %s", i+1, o.Check.ID(), want[i])
		}
	}
	if n := len(Top(outcomes[:1], 5)); n != 1 {
		t.Errorf("fewer failures than the limit: got %d, want 1", n)
	}
}

func same(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func show(v *int) string {
	if v == nil {
		return "none"
	}
	return strconv.Itoa(*v)
}
