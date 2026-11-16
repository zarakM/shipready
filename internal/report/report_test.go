package report

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
)

// buildFromFixtures runs every check against the given fixtures.
func buildFromFixtures(t *testing.T, skip []string, fixtures ...string) Report {
	t.Helper()
	var snap *collect.Snapshot
	{
		var all []string
		for _, f := range fixtures {
			all = append(all, filepath.Join("..", "..", "testdata", f))
		}
		s := collect.NewSnapshot()
		for _, path := range all {
			// A JSON fixture is the AWS side of the scan.
			if strings.HasSuffix(path, ".json") {
				a, err := collect.LoadAWSFixture(path)
				if err != nil {
					t.Fatal(err)
				}
				s.AWS = a
				continue
			}
			objs, err := collect.LoadManifests(path)
			if err != nil {
				t.Fatal(err)
			}
			part, err := collect.SnapshotFromObjects(objs)
			if err != nil {
				t.Fatal(err)
			}
			merge(s, part)
		}
		snap = s
	}
	snap.ClusterName = "demo"
	snap.KubernetesVersion = "v1.33.0"
	snap.CollectedAt = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	return Build(Input{
		ToolVersion: "test",
		ScanTime:    time.Date(2026, 11, 16, 9, 30, 0, 0, time.FixedZone("PKT", 5*3600)),
		Snapshot:    snap,
		Outcomes:    checks.Default().Run(snap, skip),
	})
}

func merge(dst, src *collect.Snapshot) {
	dst.Namespaces = append(dst.Namespaces, src.Namespaces...)
	dst.Deployments = append(dst.Deployments, src.Deployments...)
	dst.StatefulSets = append(dst.StatefulSets, src.StatefulSets...)
	dst.DaemonSets = append(dst.DaemonSets, src.DaemonSets...)
	dst.CronJobs = append(dst.CronJobs, src.CronJobs...)
	dst.Pods = append(dst.Pods, src.Pods...)
	dst.PDBs = append(dst.PDBs, src.PDBs...)
	dst.Nodes = append(dst.Nodes, src.Nodes...)
	dst.HPAs = append(dst.HPAs, src.HPAs...)
	dst.Services = append(dst.Services, src.Services...)
	dst.ServiceAccounts = append(dst.ServiceAccounts, src.ServiceAccounts...)
	dst.Roles = append(dst.Roles, src.Roles...)
	dst.ClusterRoles = append(dst.ClusterRoles, src.ClusterRoles...)
	dst.ClusterRoleBindings = append(dst.ClusterRoleBindings, src.ClusterRoleBindings...)
	dst.NetworkPolicies = append(dst.NetworkPolicies, src.NetworkPolicies...)
	dst.PVs = append(dst.PVs, src.PVs...)
	dst.PVCs = append(dst.PVCs, src.PVCs...)
	dst.PodMetrics = append(dst.PodMetrics, src.PodMetrics...)
	dst.Certificates = append(dst.Certificates, src.Certificates...)
	dst.VeleroBackups = append(dst.VeleroBackups, src.VeleroBackups...)
	dst.VeleroSchedules = append(dst.VeleroSchedules, src.VeleroSchedules...)
	dst.VolumeSnapshots = append(dst.VolumeSnapshots, src.VolumeSnapshots...)
	for g := range src.APIGroups {
		dst.APIGroups[g] = true
	}
}

var badCluster = []string{"rel/bad.yaml", "rel/cluster-bad.yaml", "sec/bad.yaml", "cost/bad.yaml", "rec/bad.yaml"}

func TestJSONSchema(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, buildFromFixtures(t, []string{"REL-004"}, badCluster...)); err != nil {
		t.Fatal(err)
	}

	// Decode generically so the test pins the wire format, not the Go types.
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	want := map[string]any{
		"schemaVersion":     "1",
		"cluster":           "demo",
		"kubernetesVersion": "v1.33.0",
		"scanTime":          "2026-11-16T04:30:00Z",
	}
	for k, v := range want {
		if doc[k] != v {
			t.Errorf("%s = %v, want %v", k, doc[k], v)
		}
	}
	if tool := doc["tool"].(map[string]any); tool["name"] != "shipready" || tool["version"] != "test" {
		t.Errorf("tool = %v", tool)
	}

	list := doc["checks"].([]any)
	if len(list) != 32 {
		t.Fatalf("checks = %d, want 32", len(list))
	}
	counts := map[string]float64{}
	for _, item := range list {
		c := item.(map[string]any)
		counts[c["status"].(string)]++
		for _, field := range []string{"id", "category", "severity", "title", "description", "remediation", "status"} {
			if s, _ := c[field].(string); s == "" {
				t.Errorf("%v: field %q is missing or empty", c["id"], field)
			}
		}
		resources, ok := c["resources"].([]any)
		if !ok {
			t.Errorf("%v: resources must always be an array, got %T", c["id"], c["resources"])
		}
		switch c["status"] {
		case "fail":
			if len(resources) == 0 {
				t.Errorf("%v: failed with no affected resources", c["id"])
			}
		case "skipped":
			if s, _ := c["skipReason"].(string); s == "" {
				t.Errorf("%v: skipped with no reason", c["id"])
			}
		}
	}
	summary := doc["summary"].(map[string]any)
	if summary["passed"] != counts["pass"] || summary["failed"] != counts["fail"] || summary["skipped"] != counts["skipped"] {
		t.Errorf("summary %v does not match the checks: %v", summary, counts)
	}
	if counts["skipped"] != 8 {
		t.Errorf("skipped = %v, want the one named with --skip plus the seven AWS checks", counts["skipped"])
	}

	first := list[0].(map[string]any)
	if first["id"] != "REL-001" {
		t.Fatalf("first check = %v, want REL-001", first["id"])
	}
	wantRes := map[string]any{
		"namespace": "shop", "kind": "CronJob", "name": "cleanup",
		"container": "job", "detail": "no CPU request",
	}
	foundRes := false
	for _, item := range first["resources"].([]any) {
		if reflect.DeepEqual(item, wantRes) {
			foundRes = true
		}
	}
	if !foundRes {
		t.Errorf("REL-001 resources do not include %v:\n%v", wantRes, first["resources"])
	}

	sc := doc["score"].(map[string]any)
	if _, ok := sc["overall"].(float64); !ok {
		t.Errorf("score.overall = %v, want a number", sc["overall"])
	}
	if g, _ := sc["grade"].(string); g == "" {
		t.Error("score.grade is missing")
	}
	cats := sc["categories"].([]any)
	if len(cats) != 4 {
		t.Fatalf("score.categories = %d, want 4", len(cats))
	}
	for i, name := range []string{"reliability", "security", "cost", "recovery"} {
		c := cats[i].(map[string]any)
		if c["category"] != name {
			t.Errorf("category %d = %v, want %s", i, c["category"], name)
		}
		for _, field := range []string{"score", "passed", "failed", "skipped"} {
			if _, ok := c[field].(float64); !ok {
				t.Errorf("%s.%s = %v, want a number", name, field, c[field])
			}
		}
	}

	top := doc["topPriorities"].([]any)
	if len(top) != 5 {
		t.Fatalf("topPriorities = %v, want 5 IDs", top)
	}
	// The two critical failures in the fixtures lead the list.
	if top[0] != "SEC-002" || top[1] != "REL-008" {
		t.Errorf("topPriorities = %v, want the critical findings SEC-002 and REL-008 first", top)
	}
}

func TestJSONWhenNothingCouldBeScored(t *testing.T) {
	snap := collect.NewSnapshot()
	rep := Build(Input{Snapshot: snap, Outcomes: checks.Default().Run(snap, []string{"reliability", "security", "cost", "recovery"})})
	var buf bytes.Buffer
	if err := WriteJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	sc := doc["score"].(map[string]any)
	if v, present := sc["overall"]; !present || v != nil {
		t.Errorf("score.overall = %v (present: %v), want an explicit null", v, present)
	}
	if top, ok := doc["topPriorities"].([]any); !ok || len(top) != 0 {
		t.Errorf("topPriorities = %v, want an empty array", doc["topPriorities"])
	}
}

func TestFailedThreshold(t *testing.T) {
	rep := Report{Checks: []Check{
		{ID: "A", Severity: checks.High, Status: checks.Fail},
		{ID: "B", Severity: checks.Critical, Status: checks.Pass},
		{ID: "C", Severity: checks.Critical, Status: checks.Skipped},
	}}
	if !rep.Failed(checks.High) {
		t.Error("a failed high check should trip the high threshold")
	}
	if rep.Failed(checks.Critical) {
		t.Error("a failed high check should not trip the critical threshold")
	}
}

func TestWriteSummary(t *testing.T) {
	var buf bytes.Buffer
	rep := buildFromFixtures(t, []string{"REL-004"}, badCluster...)
	if err := WriteSummary(&buf, rep, "./shipready-report.json"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Log("\n" + out)

	for _, want := range []string{
		"ShipReady scan of demo (Kubernetes v1.33.0)",
		"Overall score: ",
		"Reliability", "Security", "Cost", "Recovery",
		"Top priorities",
		"1. [CRITICAL] SEC-002",
		"2. [CRITICAL] REL-008 All nodes in a single availability zone (2 resources affected)",
		"Fix: Spread node groups across at least two availability zones",
		"more in the report",
		"8 checks were not run",
		"Full report: ./shipready-report.json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary is missing %q", want)
		}
	}
	if n := strings.Count(out, "Fix: "); n != 5 {
		t.Errorf("summary lists %d findings, want the top 5", n)
	}
	if strings.Contains(out, "not-a-real") {
		t.Error("summary leaks an environment variable value")
	}

	t.Run("clean cluster", func(t *testing.T) {
		var buf bytes.Buffer
		clean := buildFromFixtures(t, nil, "rel/good.yaml")
		clean.Checks, clean.TopPriorities, clean.Summary = nil, nil, Summary{Passed: 3}
		if err := WriteSummary(&buf, clean, "r.json"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "No failed checks.") {
			t.Errorf("summary should say there are no failed checks:\n%s", buf.String())
		}
	})
}
