// Package score turns check outcomes into category and overall scores. It is
// pure: the same outcomes always give the same score.
package score

import (
	"math"
	"sort"

	"github.com/zarakM/shipready/internal/checks"
)

// Weight is how much a check of the given severity counts towards its
// category score.
func Weight(s checks.Severity) int {
	switch s {
	case checks.Critical:
		return 10
	case checks.High:
		return 5
	case checks.Medium:
		return 2
	case checks.Low:
		return 1
	}
	return 0
}

// Grade maps a 0-100 score to a letter.
func Grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	}
	return "F"
}

// Category is one category's score and check counts.
type Category struct {
	Category checks.Category `json:"category"`
	// Score is nil when no check in the category ran, so there is nothing to
	// base a score on.
	Score   *int   `json:"score"`
	Grade   string `json:"grade,omitempty"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
}

// Score is the full result.
type Score struct {
	// Overall is nil when no category could be scored.
	Overall    *int       `json:"overall"`
	Grade      string     `json:"grade,omitempty"`
	Categories []Category `json:"categories"`
}

// Compute scores the outcomes. Each check counts once, however many resources
// it affects. Skipped checks are left out. A category's score is
// 100 x (1 - failed weight / weight of checks that ran); the overall score is
// the average of the categories that have one.
func Compute(outcomes []checks.Outcome) Score {
	type tally struct {
		cat           Category
		failed, total int
	}
	tallies := map[checks.Category]*tally{}
	for _, c := range checks.Categories {
		tallies[c] = &tally{cat: Category{Category: c}}
	}
	for _, o := range outcomes {
		m := o.Check.Meta()
		t, ok := tallies[m.Category]
		if !ok {
			continue
		}
		w := Weight(m.Severity)
		switch o.Result.Status {
		case checks.Pass:
			t.cat.Passed++
			t.total += w
		case checks.Fail:
			t.cat.Failed++
			t.total += w
			t.failed += w
		case checks.Skipped:
			t.cat.Skipped++
		}
	}

	var out Score
	sum, scored := 0, 0
	for _, c := range checks.Categories {
		t := tallies[c]
		if t.total > 0 {
			v := round(100 * (1 - float64(t.failed)/float64(t.total)))
			t.cat.Score = &v
			t.cat.Grade = Grade(v)
			sum += v
			scored++
		}
		out.Categories = append(out.Categories, t.cat)
	}
	if scored > 0 {
		v := round(float64(sum) / float64(scored))
		out.Overall = &v
		out.Grade = Grade(v)
	}
	return out
}

func round(f float64) int {
	return int(math.Round(f))
}

// Top returns up to n failed checks, most severe first. Ties go to the check
// affecting more resources, then to the lower ID.
func Top(outcomes []checks.Outcome, n int) []checks.Outcome {
	var failed []checks.Outcome
	for _, o := range outcomes {
		if o.Result.Status == checks.Fail {
			failed = append(failed, o)
		}
	}
	sort.SliceStable(failed, func(i, j int) bool {
		wi, wj := Weight(failed[i].Check.Meta().Severity), Weight(failed[j].Check.Meta().Severity)
		if wi != wj {
			return wi > wj
		}
		ri, rj := len(failed[i].Result.Resources), len(failed[j].Result.Resources)
		if ri != rj {
			return ri > rj
		}
		return failed[i].Check.ID() < failed[j].Check.ID()
	})
	if len(failed) > n {
		failed = failed[:n]
	}
	return failed
}
