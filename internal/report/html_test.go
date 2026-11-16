package report

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/score"
)

var update = flag.Bool("update", false, "rewrite docs/sample-report.html from the fixtures")

const (
	samplePath = "../../docs/sample-report.html"
	// The sample needs some link to show the button. It is a placeholder
	// until the Opscify booking URL exists.
	sampleCTA = "https://example.com/book-a-call"
)

func render(t *testing.T, r Report, opts HTMLOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteHTML(&buf, r, opts); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// sampleReport is the report behind docs/sample-report.html. Everything in it
// is fixed, so the file is byte-for-byte reproducible.
func sampleReport(t *testing.T) Report {
	t.Helper()
	r := buildFromFixtures(t, nil, "sample/cluster.yaml")
	r.Cluster = "acme-prod-eks"
	r.Tool.Version = "sample"
	return r
}

func TestSampleReportIsCurrent(t *testing.T) {
	r := sampleReport(t)

	// The sample is meant to show a mixed, believable result. Pin it so a
	// change to a check that shifts it is noticed.
	if r.Score.Overall == nil || *r.Score.Overall != 60 || r.Score.Grade != "C" {
		t.Errorf("sample overall = %v %s, want 60 C", r.Score.Overall, r.Score.Grade)
	}
	want := map[checks.Category]int{checks.Reliability: 60, checks.Security: 49, checks.Cost: 57, checks.Recovery: 75}
	for _, c := range r.Score.Categories {
		if c.Score == nil || *c.Score != want[c.Category] {
			t.Errorf("sample %s score = %v, want %d", c.Category, c.Score, want[c.Category])
		}
	}

	got := render(t, r, HTMLOptions{CTAURL: sampleCTA})
	if *update {
		if err := os.WriteFile(samplePath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", samplePath)
		return
	}
	committed, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("%v\nrun: go test ./internal/report -run TestSampleReportIsCurrent -update", err)
	}
	if string(committed) != got {
		t.Errorf("%s is out of date; run: go test ./internal/report -run TestSampleReportIsCurrent -update", filepath.Clean(samplePath))
	}
}

func TestHTMLSections(t *testing.T) {
	r := buildFromFixtures(t, []string{"REL-004"}, badCluster...)
	r.Namespaces = nil
	out := render(t, r, HTMLOptions{CTAURL: sampleCTA})

	for _, want := range []string{
		// 1. Header
		"<title>ShipReady report: demo</title>",
		"<h1>demo</h1>",
		"16 Nov 2026, 04:30 UTC",
		"v1.33.0",
		"Whole cluster",
		`<span class="score-number">13</span>`,
		"Grade F",
		// 2. Category cards
		`href="#cat-reliability"`, `href="#cat-security"`, `href="#cat-cost"`, `href="#cat-recovery"`,
		"1 passed · 2 failed · 0 not run",
		// 3. Top priorities
		"Top priorities",
		`<a class="check-id" href="#SEC-002">SEC-002</a>`,
		"<strong>Fix:</strong> Replace cluster-admin",
		// 4. Findings by category
		`<details class="finding sev-critical" id="REL-008" data-severity="critical">`,
		"Why it matters",
		"How to fix it",
		"Affected resources (2)",
		"zone us-east-1a",
		"topologySpreadConstraints:",
		// Backticks in check text become code.
		"ClusterRoleBindings to <code>cluster-admin</code> beyond system subjects",
		// A passed check is listed.
		`<span class="check-id">REC-001</span>`,
		// 5. Skipped checks
		"Checks that did not run",
		"skipped with --skip",
		// 6. Call to action
		"Want this fixed in a week?",
		`<a class="cta-button" href="https://example.com/book-a-call" rel="noopener noreferrer">Book a call</a>`,
		// Footer
		"Nothing in this report was sent anywhere",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report is missing %q", want)
		}
	}

	if n := strings.Count(out, `<p class="fix">`); n != TopPriorityCount {
		t.Errorf("top priorities lists %d findings, want %d", n, TopPriorityCount)
	}
	if n, want := strings.Count(out, `<details class="finding `), r.Summary.Failed; n != want {
		t.Errorf("report has %d findings, want one per failed check (%d)", n, want)
	}
	// SEC-002 is the worst security finding, so it leads its category.
	if strings.Index(out, `id="SEC-002"`) > strings.Index(out, `id="SEC-001"`) {
		t.Error("findings within a category should be ordered by severity")
	}
}

// The report must be one file that works offline and talks to nobody.
func TestHTMLIsSelfContained(t *testing.T) {
	out := render(t, sampleReport(t), HTMLOptions{CTAURL: sampleCTA})

	if !strings.Contains(out, `http-equiv="Content-Security-Policy" content="default-src 'none';`) {
		t.Error("report must carry a Content-Security-Policy that blocks external requests")
	}
	for _, banned := range []string{" src=", "<link", "@import", "url(", "<iframe", "<img", "fetch(", "XMLHttpRequest", "sendBeacon", "//cdn", "fonts.googleapis"} {
		if strings.Contains(out, banned) {
			t.Errorf("report contains %q; it must not load or send anything", banned)
		}
	}
	// Every link is either inside the page or the call to action.
	for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(out, -1) {
		if href := m[1]; !strings.HasPrefix(href, "#") && href != sampleCTA {
			t.Errorf("unexpected external link %q", href)
		}
	}
	if !strings.Contains(out, "<style>") || !strings.Contains(out, "<script>") {
		t.Error("CSS and JS must be inline")
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "ZgotmplZ") {
		t.Error("template output contains unrendered or rejected content")
	}
}

func TestHTMLEscapesClusterData(t *testing.T) {
	const attack = `<script>alert("x")</script>`
	r := Report{
		Cluster: attack,
		Tool:    Tool{Version: "test"},
		Summary: Summary{Failed: 1},
		Score:   score.Compute(nil),
		Checks: []Check{{
			ID: "REL-001", Category: checks.Reliability, Severity: checks.High, Status: checks.Fail,
			Title: "Title with `code` and " + attack, Description: attack, Remediation: attack, Snippet: attack,
			Resources: []checks.Resource{{Namespace: attack, Kind: "Pod", Name: attack, Container: attack, Detail: attack}},
		}},
		TopPriorities: []string{"REL-001"},
	}
	out := render(t, r, HTMLOptions{})

	// The page has exactly one script element: its own.
	if n := strings.Count(out, "<script"); n != 1 {
		t.Errorf("found %d <script tags, want only the report's own", n)
	}
	if strings.Contains(out, `alert("x")`) {
		t.Error("cluster-supplied text reached the page unescaped")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("cluster-supplied text should appear escaped")
	}
	if !strings.Contains(out, "<code>code</code>") {
		t.Error("backticks should still become code inside escaped text")
	}
}

func TestHTMLCallToActionLink(t *testing.T) {
	r := sampleReport(t)
	cases := []struct {
		name   string
		url    string
		button bool
	}{
		{"https link", "https://opscify.example/book", true},
		{"unset hides the button", "", false},
		{"javascript scheme is refused", "javascript:alert(1)", false},
		{"relative path is refused", "/book", false},
		{"data scheme is refused", "data:text/html,hi", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := render(t, r, HTMLOptions{CTAURL: tc.url})
			if got := strings.Contains(out, `class="cta-button"`); got != tc.button {
				t.Errorf("button shown = %v, want %v", got, tc.button)
			}
			if !strings.Contains(out, "Want this fixed in a week?") {
				t.Error("the call-to-action text is always shown")
			}
			if tc.url != "" && !tc.button && strings.Contains(out, tc.url) {
				t.Error("a refused link must not appear anywhere in the page")
			}
		})
	}
}

func TestHTMLEdgeCases(t *testing.T) {
	t.Run("nothing could be scored", func(t *testing.T) {
		snap := collect.NewSnapshot()
		r := Build(Input{Snapshot: snap, Outcomes: checks.Default().Run(snap, []string{"reliability", "security", "cost", "recovery"})})
		out := render(t, r, HTMLOptions{})
		for _, want := range []string{"n/a", "No check could run", "No check in this category could run.", "No check failed.", "Kubernetes cluster"} {
			if !strings.Contains(out, want) {
				t.Errorf("report is missing %q", want)
			}
		}
		if strings.Contains(out, `class="ring-value"`) || strings.Contains(out, `data-filter="all"`) {
			t.Error("an unscored report with no findings should have no score ring and no filter bar")
		}
	})

	t.Run("no secret values", func(t *testing.T) {
		out := render(t, buildFromFixtures(t, nil, badCluster...), HTMLOptions{})
		if strings.Contains(out, "not-a-real") {
			t.Error("an environment variable value reached the report")
		}
		if !strings.Contains(out, "DB_PASSWORD") {
			t.Error("the variable name should still be reported")
		}
	})

	t.Run("namespace scope is shown", func(t *testing.T) {
		r := sampleReport(t)
		r.Namespaces = []string{"prod", "staging"}
		if out := render(t, r, HTMLOptions{}); !strings.Contains(out, "prod, staging") {
			t.Error("the scanned namespaces should appear in the header")
		}
	})
}

func TestInlineCode(t *testing.T) {
	cases := map[string]string{
		"plain":                "plain",
		"use `kubectl get`":    "use <code>kubectl get</code>",
		"`a` and `b`":          "<code>a</code> and <code>b</code>",
		"a < b & `c > d`":      "a &lt; b &amp; <code>c &gt; d</code>",
		"unmatched ` backtick": "unmatched ` backtick",
		"<b>bold</b> `<i>`":    "&lt;b&gt;bold&lt;/b&gt; <code>&lt;i&gt;</code>",
	}
	for in, want := range cases {
		if got := string(inlineCode(in)); got != want {
			t.Errorf("inlineCode(%q) = %q, want %q", in, got, want)
		}
	}
}
