package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
	"github.com/muhammadzarakbinkaleem/shipready/internal/report"
)

// fakeCluster returns a connectFunc serving the given fixtures.
func fakeCluster(t *testing.T, fixtures ...string) connectFunc {
	t.Helper()
	var objs []runtime.Object
	for _, name := range fixtures {
		loaded, err := collect.LoadManifests(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		objs = append(objs, loaded...)
	}
	return func(string) (kubernetes.Interface, string, error) {
		return fake.NewClientset(objs...), "demo-cluster", nil
	}
}

func run(t *testing.T, connect connectFunc, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd("test", connect)
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

func TestScanWritesJSONReport(t *testing.T) {
	cases := []struct {
		name     string
		fixtures []string
		args     []string
		want     map[string]string // check ID -> status
	}{
		{
			name:     "healthy cluster passes everything",
			fixtures: []string{"rel/good.yaml"},
			want:     map[string]string{"REL-001": "pass", "REL-002": "pass", "REL-003": "pass", "REL-004": "pass", "REL-005": "pass"},
		},
		{
			name:     "problem cluster fails everything",
			fixtures: []string{"rel/good.yaml", "rel/bad.yaml"},
			want:     map[string]string{"REL-001": "fail", "REL-002": "fail", "REL-003": "fail", "REL-004": "fail", "REL-005": "fail"},
		},
		{
			name:     "skip flag",
			fixtures: []string{"rel/bad.yaml"},
			args:     []string{"--skip", "REL-002,rel-005"},
			want:     map[string]string{"REL-001": "fail", "REL-002": "skipped", "REL-003": "fail", "REL-004": "fail", "REL-005": "skipped"},
		},
		{
			name:     "namespace filter with no workloads in it",
			fixtures: []string{"rel/bad.yaml"},
			args:     []string{"--namespace", "empty"},
			want:     map[string]string{"REL-001": "pass", "REL-002": "pass", "REL-003": "pass", "REL-004": "pass", "REL-005": "pass"},
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
			if len(rep.Checks) != len(tc.want) {
				t.Fatalf("checks = %d, want %d", len(rep.Checks), len(tc.want))
			}
			for _, c := range rep.Checks {
				if string(c.Status) != tc.want[c.ID] {
					t.Errorf("%s = %s, want %s", c.ID, c.Status, tc.want[c.ID])
				}
			}
			if !strings.Contains(out, "demo-cluster") || !strings.Contains(out, path) {
				t.Errorf("terminal output should name the cluster and the report path:\n%s", out)
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
		{"html not available yet", []string{"--format", "html"}, "not available yet"},
		{"unknown format", []string{"--format", "pdf"}, "unknown format"},
		{"unknown skip value", []string{"--skip", "REL-999"}, "REL-999"},
		{"positional argument", []string{"extra"}, "unknown command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connected := false
			connect := func(string) (kubernetes.Interface, string, error) {
				connected = true
				return fake.NewClientset(), "demo", nil
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
	connect := func(string) (kubernetes.Interface, string, error) {
		return nil, "", errors.New("kubeconfig context \"nope\" not found")
	}
	if _, err := run(t, connect, "scan", "--context", "nope"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestChecksAndVersionCommands(t *testing.T) {
	out, err := run(t, nil, "checks")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"REL-001", "REL-002", "REL-003", "REL-004", "REL-005"} {
		if !strings.Contains(out, id) {
			t.Errorf("checks output is missing %s:\n%s", id, out)
		}
	}

	out, err = run(t, nil, "version")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "shipready test" {
		t.Errorf("version output = %q", out)
	}
}
