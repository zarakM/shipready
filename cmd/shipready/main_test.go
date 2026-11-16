package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/collect/collecttest"
	"github.com/zarakM/shipready/internal/report"
)

const root = "../.."

// A cluster with problems in every category, and one with none that the
// fixtures can express.
var (
	badCluster  = []string{"rel/bad.yaml", "rel/cluster-bad.yaml", "sec/bad.yaml", "cost/bad.yaml", "rec/bad.yaml"}
	goodCluster = []string{"rel/good.yaml"}
)

// fakeCluster returns a connectFunc serving the given fixtures.
func fakeCluster(t *testing.T, fixtures ...string) connectFunc {
	t.Helper()
	objs := collecttest.Load(t, root, fixtures...)
	return func(string) (collect.Clients, string, error) {
		return collecttest.NewCluster(t, objs...).Clients(), "demo-cluster", nil
	}
}

func run(t *testing.T, connect connectFunc, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(buildInfo{Version: "test", CTAURL: "https://example.com/book-a-call"}, connect)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func readReport(t *testing.T, path string) report.Report {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rep report.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	return rep
}

func statuses(rep report.Report) map[string]string {
	out := map[string]string{}
	for _, c := range rep.Checks {
		out[c.ID] = string(c.Status)
	}
	return out
}

func TestScanWritesJSONReport(t *testing.T) {
	cases := []struct {
		name     string
		fixtures []string
		args     []string
		want     map[string]string // check ID -> status, for the checks that matter to the case
	}{
		{
			name:     "healthy workloads",
			fixtures: goodCluster,
			want: map[string]string{
				"REL-001": "pass", "REL-002": "pass", "REL-003": "pass", "REL-004": "pass", "REL-005": "pass",
				// Add-ons that are not installed lead to skips, not failures.
				"REL-009": "skipped", "COST-001": "skipped", "REC-002": "skipped",
			},
		},
		{
			name:     "problems in every category",
			fixtures: badCluster,
			want: map[string]string{
				"REL-001": "fail", "REL-002": "fail", "REL-003": "fail", "REL-004": "fail", "REL-005": "fail",
				"REL-006": "fail", "REL-007": "fail", "REL-008": "fail", "REL-009": "fail", "REL-010": "fail",
				"SEC-001": "fail", "SEC-002": "fail", "SEC-003": "fail", "SEC-004": "fail",
				"SEC-005": "fail", "SEC-006": "fail", "SEC-007": "fail", "SEC-008": "fail",
				"COST-001": "fail", "COST-002": "fail", "COST-003": "fail", "COST-004": "fail",
				"REC-001": "pass", "REC-002": "fail", "REC-003": "fail",
			},
		},
		{
			name:     "skip flag",
			fixtures: badCluster,
			args:     []string{"--skip", "REL-002,rel-005,cost"},
			want: map[string]string{
				"REL-001": "fail", "REL-002": "skipped", "REL-005": "skipped",
				"COST-001": "skipped", "COST-002": "skipped", "COST-003": "skipped", "COST-004": "skipped",
				"SEC-001": "fail",
			},
		},
		{
			name:     "namespace filter",
			fixtures: badCluster,
			args:     []string{"--namespace", "billing"},
			want: map[string]string{
				// billing is set up correctly in the fixtures.
				"SEC-001": "pass", "SEC-004": "pass", "SEC-005": "pass", "SEC-008": "pass",
				// Checks that need the whole cluster say so.
				"COST-002": "skipped", "COST-004": "skipped", "REC-001": "skipped",
				// Cluster-scoped findings are still reported.
				"SEC-002": "fail", "REL-008": "fail",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			args := append([]string{"scan", "--output", path}, tc.args...)
			out, err := run(t, fakeCluster(t, tc.fixtures...), args...)
			if err != nil {
				t.Fatalf("scan failed: %v\n%s", err, out)
			}
			rep := readReport(t, path)
			if rep.Cluster != "demo-cluster" || rep.Tool.Version != "test" || rep.ScanTime == "" {
				t.Errorf("header = cluster %q, version %q, time %q", rep.Cluster, rep.Tool.Version, rep.ScanTime)
			}
			if len(rep.Checks) != 25 {
				t.Fatalf("checks = %d, want 25", len(rep.Checks))
			}
			got := statuses(rep)
			for id, want := range tc.want {
				if got[id] != want {
					t.Errorf("%s = %s, want %s", id, got[id], want)
				}
			}
			if rep.Score.Overall == nil || len(rep.Score.Categories) != 4 {
				t.Errorf("report has no score: %+v", rep.Score)
			}
			for _, want := range []string{"demo-cluster", "Overall score:", path} {
				if !strings.Contains(out, want) {
					t.Errorf("terminal output is missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestScanFailOn(t *testing.T) {
	cases := []struct {
		name     string
		fixtures []string
		args     []string
		tripped  bool
	}{
		{"no flag never fails the run", badCluster, nil, false},
		{"critical findings trip critical", badCluster, []string{"--fail-on", "critical"}, true},
		{"critical findings trip high", badCluster, []string{"--fail-on", "high"}, true},
		{"high findings do not trip critical", []string{"rel/bad.yaml"}, []string{"--fail-on", "critical"}, false},
		{"high findings trip high", []string{"rel/bad.yaml"}, []string{"--fail-on", "HIGH"}, true},
		{"clean workloads do not trip high", goodCluster, []string{"--fail-on", "high", "--skip", "security"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			args := append([]string{"scan", "--output", path}, tc.args...)
			out, err := run(t, fakeCluster(t, tc.fixtures...), args...)

			var failOn *failOnError
			if got := errors.As(err, &failOn); got != tc.tripped {
				t.Fatalf("fail-on tripped = %v (err: %v), want %v\n%s", got, err, tc.tripped, out)
			}
			if err != nil && !tc.tripped {
				t.Fatalf("unexpected error: %v", err)
			}
			// The report and summary are written either way.
			if _, statErr := os.Stat(path); statErr != nil {
				t.Errorf("report was not written: %v", statErr)
			}
			if !strings.Contains(out, "Overall score:") {
				t.Errorf("summary was not printed:\n%s", out)
			}
		})
	}
}

func TestScanRejectsBadInput(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"empty format", []string{"--format", " "}, "at least one value"},
		{"unknown format", []string{"--format", "pdf"}, "unknown format"},
		{"unknown skip value", []string{"--skip", "REL-999"}, "REL-999"},
		{"unknown fail-on value", []string{"--fail-on", "medium"}, "critical or high"},
		{"positional argument", []string{"extra"}, "unknown command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connected := false
			connect := func(string) (collect.Clients, string, error) {
				connected = true
				return collecttest.NewCluster(t).Clients(), "demo", nil
			}
			path := filepath.Join(t.TempDir(), "report.json")
			_, err := run(t, connect, append([]string{"scan", "--output", path}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
			if connected {
				t.Error("input must be validated before connecting to the cluster")
			}
			if _, statErr := os.Stat(path); statErr == nil {
				t.Error("no report should be written when the input is invalid")
			}
		})
	}
}

func TestScanReportsConnectionFailure(t *testing.T) {
	connect := func(string) (collect.Clients, string, error) {
		return collect.Clients{}, "", errors.New("kubeconfig context \"nope\" not found")
	}
	if _, err := run(t, connect, "scan", "--context", "nope"); err == nil {
		t.Fatal("expected an error")
	}
}

// The scan must never write a secret value anywhere: not the report, not the
// terminal.
func TestScanNeverOutputsSecretValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	out, err := run(t, fakeCluster(t, badCluster...), "scan", "--output", path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// sec/bad.yaml sets DB_PASSWORD and API_TOKEN to values starting with this.
	const marker = "not-a-real"
	if strings.Contains(string(data), marker) || strings.Contains(out, marker) {
		t.Error("an environment variable value reached the output")
	}
	if !strings.Contains(string(data), "DB_PASSWORD") {
		t.Error("the report should still name the variable")
	}
}

func TestChecksAndVersionCommands(t *testing.T) {
	out, err := run(t, nil, "checks")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"REL-001", "REL-010", "SEC-001", "SEC-008", "COST-001", "COST-004", "REC-001", "REC-003"} {
		if !strings.Contains(out, id) {
			t.Errorf("checks output is missing %s:\n%s", id, out)
		}
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines != 25 {
		t.Errorf("checks output has %d rows after the header, want 25", lines)
	}

	out, err = run(t, nil, "version")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "shipready test" {
		t.Errorf("version output = %q", out)
	}
}

func TestScanOutputFormats(t *testing.T) {
	cases := []struct {
		name     string
		output   string   // --output, relative to the temp dir; "" leaves the flag off
		args     []string // extra flags
		wantHTML string   // file expected to hold the HTML report, "" for none
		wantJSON string
	}{
		{name: "html is the default", output: "", wantHTML: "shipready-report.html"},
		{name: "html to a chosen path", output: "out/cluster.html", wantHTML: "out/cluster.html"},
		{name: "a .json output path means json", output: "r.json", wantJSON: "r.json"},
		{name: "explicit format wins over the extension", output: "r.json", args: []string{"--format", "html"}, wantHTML: "r.json"},
		{name: "json only", output: "r.txt", args: []string{"--format", "json"}, wantJSON: "r.txt"},
		{name: "both formats share the name", output: "cluster.html", args: []string{"--format", "html,json"}, wantHTML: "cluster.html", wantJSON: "cluster.json"},
		{name: "both formats, no extension given", output: "cluster", args: []string{"--format", "json", "--format", "HTML"}, wantHTML: "cluster.html", wantJSON: "cluster.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Fixtures are loaded relative to the package, so before moving.
			connect := fakeCluster(t, badCluster...)
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)

			args := []string{"scan"}
			if tc.output != "" {
				args = append(args, "--output", tc.output)
			}
			out, err := run(t, connect, append(args, tc.args...)...)
			if err != nil {
				t.Fatalf("scan failed: %v\n%s", err, out)
			}

			var files []string
			err = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					files = append(files, filepath.ToSlash(path))
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.wantHTML != "" {
				want++
				data, err := os.ReadFile(tc.wantHTML)
				if err != nil {
					t.Fatalf("HTML report missing: %v (files: %v)", err, files)
				}
				html := string(data)
				if !strings.HasPrefix(html, "<!doctype html>") || !strings.HasSuffix(strings.TrimSpace(html), "</html>") {
					t.Error("HTML report is not a complete document")
				}
				for _, part := range []string{"demo-cluster", "Top priorities", "SEC-002", `href="https://example.com/book-a-call"`} {
					if !strings.Contains(html, part) {
						t.Errorf("HTML report is missing %q", part)
					}
				}
				if !strings.Contains(out, tc.wantHTML) {
					t.Errorf("terminal output should name %s:\n%s", tc.wantHTML, out)
				}
			}
			if tc.wantJSON != "" {
				want++
				if rep := readReport(t, tc.wantJSON); len(rep.Checks) != 25 {
					t.Errorf("JSON report has %d checks, want 25", len(rep.Checks))
				}
				if !strings.Contains(out, tc.wantJSON) {
					t.Errorf("terminal output should name %s:\n%s", tc.wantJSON, out)
				}
			}
			// Only the reports: no temporary files left behind.
			if len(files) != want {
				t.Errorf("files written = %v, want exactly %d report(s)", files, want)
			}
		})
	}
}

// A report that cannot be written must not leave a partial file, and must not
// leave one format behind without the other.
func TestScanWritesNothingOnFailure(t *testing.T) {
	connect := fakeCluster(t, goodCluster...)
	dir := t.TempDir()
	t.Chdir(dir)
	// "blocked.json" is a directory, so the JSON report cannot be renamed
	// into place. The HTML report is rendered first but must not survive.
	if err := os.Mkdir("blocked.json", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("blocked.json", "keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, connect, "scan", "--output", "blocked", "--format", "html,json")
	if err == nil {
		t.Fatal("expected an error when a report cannot be written")
	}
	entries, readErr := os.ReadDir(".")
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, e := range entries {
		if e.Name() != "blocked.json" {
			t.Errorf("unexpected file left behind: %s", e.Name())
		}
	}
}
