package checks

import (
	"strings"
	"testing"

	"github.com/zarakM/shipready/internal/collect"
)

// specChecks is the check table from docs/SPEC.md section 6, Kubernetes
// checks only. The registry must match it exactly.
var specChecks = map[string]struct {
	category Category
	severity Severity
}{
	"REL-001": {Reliability, High}, "REL-002": {Reliability, High}, "REL-003": {Reliability, High},
	"REL-004": {Reliability, Medium}, "REL-005": {Reliability, Medium}, "REL-006": {Reliability, High},
	"REL-007": {Reliability, Low}, "REL-008": {Reliability, Critical}, "REL-009": {Reliability, High},
	"REL-010": {Reliability, Medium},
	"SEC-001": {Security, High}, "SEC-002": {Security, Critical}, "SEC-003": {Security, High},
	"SEC-004": {Security, High}, "SEC-005": {Security, Medium}, "SEC-006": {Security, Medium},
	"SEC-007": {Security, High}, "SEC-008": {Security, Low},
	"COST-001": {Cost, Medium}, "COST-002": {Cost, Medium}, "COST-003": {Cost, Low}, "COST-004": {Cost, Medium},
	"REC-001": {Recovery, Critical}, "REC-002": {Recovery, High}, "REC-003": {Recovery, High},
}

func TestDefaultRegistryMatchesSpec(t *testing.T) {
	all := Default().All()
	if len(all) != len(specChecks) {
		t.Errorf("registered checks = %d, want %d", len(all), len(specChecks))
	}
	for _, c := range all {
		m := c.Meta()
		want, ok := specChecks[c.ID()]
		if !ok {
			t.Errorf("%s is registered but not in the spec", c.ID())
			continue
		}
		if m.Category != want.category || m.Severity != want.severity {
			t.Errorf("%s = %s/%s, spec says %s/%s", c.ID(), m.Category, m.Severity, want.category, want.severity)
		}
		if m.Title == "" || m.Description == "" || m.Remediation == "" {
			t.Errorf("%s: title, description and remediation are all required", c.ID())
		}
		if strings.ContainsAny(m.Remediation, "\n") {
			t.Errorf("%s: remediation is shown as a one-line fix and must not contain line breaks", c.ID())
		}
	}
}

// Every check must cope with a cluster that has nothing in it, and with one
// where nothing could be listed, without panicking.
func TestChecksHandleEmptyAndUnreadableClusters(t *testing.T) {
	unreadable := collect.NewSnapshot()
	for _, res := range []string{
		collect.ResNamespaces, collect.ResDeployments, collect.ResStatefulSets, collect.ResDaemonSets,
		collect.ResCronJobs, collect.ResPods, collect.ResPDBs, collect.ResNodes, collect.ResHPAs,
		collect.ResServices, collect.ResServiceAccounts, collect.ResRoles, collect.ResClusterRoles,
		collect.ResClusterRoleBindings, collect.ResNetworkPolicies, collect.ResPVs, collect.ResPVCs,
	} {
		unreadable.Errors[res] = errDenied(res)
	}
	limited := collect.NewSnapshot()
	limited.ClusterWide = false

	// An empty cluster really does have no node autoscaler, so COST-004 is
	// right to fail there.
	failsWhenEmpty := map[string]bool{"COST-004": true}

	for _, c := range Default().All() {
		if got := c.Run(collect.NewSnapshot()); got.Status == Fail && !failsWhenEmpty[c.ID()] {
			t.Errorf("%s fails on an empty cluster: %v", c.ID(), got.Resources)
		}
		if got := c.Run(unreadable); got.Status != Skipped || got.SkipReason == "" {
			t.Errorf("%s = %s on a cluster where nothing can be listed, want skipped with a reason", c.ID(), got.Status)
		}
		if got := c.Run(limited); got.Status == Fail {
			t.Errorf("%s fails on an empty namespace-limited scan: %v", c.ID(), got.Resources)
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
	var got []string
	for _, c := range Default().All() {
		got = append(got, c.ID())
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"REL-001 REL-002", "REL-010 SEC-001", "SEC-008 COST-001", "COST-004 REC-001"} {
		if !strings.Contains(joined, want) {
			t.Errorf("order %q does not contain %q", joined, want)
		}
	}
}

func TestRegistrySkip(t *testing.T) {
	r := Default()
	snap := collect.NewSnapshot()
	baseline := 0
	for _, o := range r.Run(snap, nil) {
		if o.Result.Status == Skipped {
			baseline++
		}
	}

	cases := []struct {
		name      string
		skip      []string
		wantExtra int // skipped checks beyond those that skip themselves
		wantErr   bool
	}{
		{"nothing skipped", nil, 0, false},
		{"by ID, case-insensitive", []string{"rel-001", "REL-003"}, 2, false},
		{"by category", []string{"Security"}, 8, false},
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
			if skippedCount-baseline != tc.wantExtra {
				t.Errorf("extra skipped = %d, want %d", skippedCount-baseline, tc.wantExtra)
			}
		})
	}
}
