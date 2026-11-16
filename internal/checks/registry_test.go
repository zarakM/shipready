package checks

import (
	"regexp"
	"testing"

	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

func TestDefaultRegistryMetadata(t *testing.T) {
	idPattern := regexp.MustCompile(`^(REL|SEC|COST|REC|AWS)-\d{3}$`)
	severities := map[Severity]bool{Critical: true, High: true, Medium: true, Low: true}
	categories := map[Category]bool{}
	for _, c := range Categories {
		categories[c] = true
	}

	all := Default().All()
	if len(all) != 5 {
		t.Errorf("registered checks = %d, want 5 for milestone 1", len(all))
	}
	for _, c := range all {
		m := c.Meta()
		if !idPattern.MatchString(c.ID()) {
			t.Errorf("%s: ID does not match the stable ID format", c.ID())
		}
		if !severities[m.Severity] {
			t.Errorf("%s: unknown severity %q", c.ID(), m.Severity)
		}
		if !categories[m.Category] {
			t.Errorf("%s: unknown category %q", c.ID(), m.Category)
		}
		if m.Title == "" || m.Description == "" || m.Remediation == "" {
			t.Errorf("%s: title, description and remediation are all required", c.ID())
		}
	}
}

func TestNewRegistryRejectsBadIDs(t *testing.T) {
	cases := []struct {
		name   string
		checks []Check
	}{
		{"duplicate ID", []Check{rel001, rel001}},
		{"empty ID", []Check{check{meta: Meta{Title: "nameless"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRegistry(tc.checks...); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestRegistryOrdersByCategoryThenID(t *testing.T) {
	stub := func(id string, cat Category) Check {
		return check{id: id, meta: Meta{Category: cat}}
	}
	r, err := NewRegistry(stub("COST-001", Cost), stub("REL-002", Reliability), stub("SEC-001", Security), stub("REL-001", Reliability))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"REL-001", "REL-002", "SEC-001", "COST-001"}
	for i, c := range r.All() {
		if c.ID() != want[i] {
			t.Errorf("position %d = %s, want %s", i, c.ID(), want[i])
		}
	}
}

func TestRegistrySkip(t *testing.T) {
	r := Default()
	snap := collect.NewSnapshot()

	cases := []struct {
		name        string
		skip        []string
		wantSkipped int
		wantErr     bool
	}{
		{"nothing skipped", nil, 0, false},
		{"by ID, case-insensitive", []string{"rel-001", "REL-003"}, 2, false},
		{"by category", []string{"Reliability"}, 5, false},
		{"category with no checks yet is valid", []string{"cost"}, 0, false},
		{"unknown value", []string{"REL-999"}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.ValidateSkip(tc.skip)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateSkip error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			skippedCount := 0
			for _, o := range r.Run(snap, tc.skip) {
				if o.Result.Status == Skipped {
					skippedCount++
					if o.Result.SkipReason == "" {
						t.Errorf("%s: skipped without a reason", o.Check.ID())
					}
				}
			}
			if skippedCount != tc.wantSkipped {
				t.Errorf("skipped = %d, want %d", skippedCount, tc.wantSkipped)
			}
		})
	}
}
