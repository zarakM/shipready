package report

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/muhammadzarakbinkaleem/shipready/internal/checks"
	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

func buildFromFixture(t *testing.T, skip []string) Report {
	t.Helper()
	objs, err := collect.LoadManifests(filepath.Join("..", "..", "testdata", "rel", "bad.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := collect.SnapshotFromObjects(objs)
	if err != nil {
		t.Fatal(err)
	}
	snap.ClusterName = "demo"
	snap.KubernetesVersion = "v1.33.0"
	return Build(Input{
		ToolVersion: "test",
		ScanTime:    time.Date(2026, 11, 16, 9, 30, 0, 0, time.FixedZone("PKT", 5*3600)),
		Snapshot:    snap,
		Outcomes:    checks.Default().Run(snap, skip),
	})
}

func TestJSONSchema(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, buildFromFixture(t, []string{"REL-004"})); err != nil {
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
	summary := doc["summary"].(map[string]any)
	if summary["passed"] != 0.0 || summary["failed"] != 4.0 || summary["skipped"] != 1.0 {
		t.Errorf("summary = %v, want 0 passed, 4 failed, 1 skipped", summary)
	}

	list := doc["checks"].([]any)
	if len(list) != 5 {
		t.Fatalf("checks = %d, want 5", len(list))
	}
	for _, item := range list {
		c := item.(map[string]any)
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

	first := list[0].(map[string]any)
	if first["id"] != "REL-001" {
		t.Fatalf("first check = %v, want REL-001", first["id"])
	}
	res := first["resources"].([]any)[0].(map[string]any)
	wantRes := map[string]any{
		"namespace": "shop", "kind": "CronJob", "name": "cleanup",
		"container": "job", "detail": "no CPU request",
	}
	for k, v := range wantRes {
		if res[k] != v {
			t.Errorf("resource %s = %v, want %v", k, res[k], v)
		}
	}
}
