package report

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/score"
)

//go:embed assets/report.html.tmpl assets/report.css assets/report.js
var assets embed.FS

// HTMLOptions are the build-time settings of the HTML report.
type HTMLOptions struct {
	// CTAURL is where the "Book a call" button points. When it is empty or
	// not an http(s) URL the button is left out, so a report never carries a
	// dead link.
	CTAURL string
}

var categoryBlurbs = map[checks.Category]string{
	checks.Reliability: "Will it stay up?",
	checks.Security:    "Who can get in, and how far?",
	checks.Cost:        "Are you paying for capacity nobody uses?",
	checks.Recovery:    "Can you get your data back?",
}

var severityLabels = map[checks.Severity]string{
	checks.Critical: "Critical",
	checks.High:     "High",
	checks.Medium:   "Medium",
	checks.Low:      "Low",
}

// The score ring is an SVG circle of radius 52.
const ringLength = 2 * math.Pi * 52

type htmlData struct {
	CSS template.CSS
	JS  template.JS

	Cluster           string
	KubernetesVersion string
	Version           string
	ScanTime          string
	Namespaces        string
	Summary           Summary
	Total             int

	HasScore   bool
	Overall    int
	Grade      string
	GradeClass string
	RingLength string
	RingOffset string

	Categories []htmlCategory
	Top        []htmlFinding
	Skipped    []Check
	CTAURL     string
}

type htmlCategory struct {
	Key        string
	Name       string
	Blurb      string
	HasScore   bool
	Score      int
	Grade      string
	GradeClass string
	Passed     int
	Failed     int
	Skipped    int

	Findings     []htmlFinding
	PassedChecks []Check
}

type htmlFinding struct {
	Check
	SeverityLabel string
	Affected      string
	ShowNamespace bool
	ShowContainer bool
}

var htmlTemplate = template.Must(
	template.New("report.html.tmpl").
		Funcs(template.FuncMap{"md": inlineCode}).
		ParseFS(assets, "assets/report.html.tmpl"),
)

// inlineCode escapes text and turns `backticked` spans into <code> elements.
// Check titles and remediation text use backticks for field names.
func inlineCode(text string) template.HTML {
	parts := strings.Split(text, "`")
	if len(parts)%2 == 0 {
		// An unmatched backtick: show the text exactly as written.
		return template.HTML(template.HTMLEscapeString(text))
	}
	var b strings.Builder
	for i, part := range parts {
		escaped := template.HTMLEscapeString(part)
		if i%2 == 1 {
			b.WriteString("<code>" + escaped + "</code>")
		} else {
			b.WriteString(escaped)
		}
	}
	return template.HTML(b.String())
}

func gradeClass(grade string) string {
	if grade == "" {
		return "grade-none"
	}
	return "grade-" + strings.ToLower(grade)
}

// safeLink returns the URL only if it is an absolute http or https link.
func safeLink(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return u.String()
}

func newFinding(c Check) htmlFinding {
	f := htmlFinding{
		Check:         c,
		SeverityLabel: severityLabels[c.Severity],
		Affected:      affected(len(c.Resources)),
	}
	for _, r := range c.Resources {
		f.ShowNamespace = f.ShowNamespace || r.Namespace != ""
		f.ShowContainer = f.ShowContainer || r.Container != ""
	}
	return f
}

func buildHTMLData(r Report, opts HTMLOptions) (htmlData, error) {
	css, err := assets.ReadFile("assets/report.css")
	if err != nil {
		return htmlData{}, err
	}
	js, err := assets.ReadFile("assets/report.js")
	if err != nil {
		return htmlData{}, err
	}

	d := htmlData{
		// These are the report's own embedded assets, not user data.
		CSS: template.CSS(css),
		JS:  template.JS(js),

		Cluster:           r.Cluster,
		KubernetesVersion: r.KubernetesVersion,
		Version:           r.Tool.Version,
		ScanTime:          r.ScanTime,
		Namespaces:        strings.Join(r.Namespaces, ", "),
		Summary:           r.Summary,
		Total:             len(r.Checks),
		GradeClass:        gradeClass(r.Score.Grade),
		Grade:             r.Score.Grade,
		RingLength:        fmt.Sprintf("%.1f", ringLength),
		CTAURL:            safeLink(opts.CTAURL),
	}
	if d.Cluster == "" {
		d.Cluster = "Kubernetes cluster"
	}
	if t, err := time.Parse(time.RFC3339, r.ScanTime); err == nil {
		d.ScanTime = t.UTC().Format("2 Jan 2006, 15:04 UTC")
	}
	if r.Score.Overall != nil {
		d.HasScore = true
		d.Overall = *r.Score.Overall
		d.RingOffset = fmt.Sprintf("%.1f", ringLength*(1-float64(d.Overall)/100))
	}

	byID := map[string]Check{}
	byCategory := map[checks.Category]*htmlCategory{}
	for _, cs := range r.Score.Categories {
		cat := htmlCategory{
			Key:        string(cs.Category),
			Name:       categoryNames[cs.Category],
			Blurb:      categoryBlurbs[cs.Category],
			Grade:      cs.Grade,
			GradeClass: gradeClass(cs.Grade),
			Passed:     cs.Passed,
			Failed:     cs.Failed,
			Skipped:    cs.Skipped,
		}
		if cs.Score != nil {
			cat.HasScore = true
			cat.Score = *cs.Score
		}
		d.Categories = append(d.Categories, cat)
	}
	for i := range d.Categories {
		byCategory[checks.Category(d.Categories[i].Key)] = &d.Categories[i]
	}

	for _, c := range r.Checks {
		byID[c.ID] = c
		cat := byCategory[c.Category]
		switch c.Status {
		case checks.Fail:
			if cat != nil {
				cat.Findings = append(cat.Findings, newFinding(c))
			}
		case checks.Pass:
			if cat != nil {
				cat.PassedChecks = append(cat.PassedChecks, c)
			}
		case checks.Skipped:
			d.Skipped = append(d.Skipped, c)
		}
	}
	// Within a category, the most severe findings come first.
	for i := range d.Categories {
		f := d.Categories[i].Findings
		sort.SliceStable(f, func(a, b int) bool {
			wa, wb := score.Weight(f[a].Severity), score.Weight(f[b].Severity)
			if wa != wb {
				return wa > wb
			}
			return f[a].ID < f[b].ID
		})
	}
	for _, id := range r.TopPriorities {
		if c, ok := byID[id]; ok {
			d.Top = append(d.Top, newFinding(c))
		}
	}
	return d, nil
}

// RenderHTML returns the complete, self-contained HTML report. Everything is
// inline: the file makes no network requests and works offline.
func RenderHTML(r Report, opts HTMLOptions) ([]byte, error) {
	data, err := buildHTMLData(r, opts)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := htmlTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering HTML report: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteHTML renders the report to w. Nothing is written if rendering fails,
// so a reader never gets half a report.
func WriteHTML(w io.Writer, r Report, opts HTMLOptions) error {
	out, err := RenderHTML(r, opts)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
