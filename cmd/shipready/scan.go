package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/report"
)

const (
	formatHTML = "html"
	formatJSON = "json"

	defaultOutput = "./shipready-report.html"
)

type scanOptions struct {
	kubeContext string
	namespaces  []string
	output      string
	formats     []string
	skip        []string
	failOn      string
}

func newScanCmd(build buildInfo, connect connectFunc) *cobra.Command {
	var opts scanOptions
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan the cluster and write a report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// `--output report.json` on its own means the user wants JSON.
			if !cmd.Flags().Changed("format") && strings.EqualFold(filepath.Ext(opts.output), ".json") {
				opts.formats = []string{formatJSON}
			}
			return runScan(cmd, build, connect, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.kubeContext, "context", "", "kubeconfig context (default: current)")
	f.StringSliceVar(&opts.namespaces, "namespace", nil, "limit to namespaces (default: all non-system)")
	f.StringVar(&opts.output, "output", defaultOutput, "report path")
	f.StringSliceVar(&opts.formats, "format", []string{formatHTML}, "report formats: html, json")
	f.StringSliceVar(&opts.skip, "skip", nil, "check IDs or categories to skip")
	f.StringVar(&opts.failOn, "fail-on", "", "exit with code 2 if a check of this severity or above fails: critical or high")
	return cmd
}

// parseFormats normalises --format, keeping the order given and dropping
// repeats.
func parseFormats(values []string) ([]string, error) {
	var out []string
	for _, v := range dedupe(values) {
		switch f := strings.ToLower(v); f {
		case formatHTML, formatJSON:
			out = append(out, f)
		default:
			return nil, fmt.Errorf("unknown format %q; supported: html, json", v)
		}
	}
	out = dedupe(out)
	if len(out) == 0 {
		return nil, errors.New("--format needs at least one value: html or json")
	}
	return out, nil
}

// outputPaths decides where each format is written. A single format goes to
// --output exactly as given. With several, each gets --output's name with its
// own extension, so `--format html,json` writes report.html and report.json
// side by side.
func outputPaths(output string, formats []string) map[string]string {
	paths := map[string]string{}
	if len(formats) == 1 {
		paths[formats[0]] = output
		return paths
	}
	base := output
	switch strings.ToLower(filepath.Ext(output)) {
	case ".html", ".htm", ".json":
		base = strings.TrimSuffix(output, filepath.Ext(output))
	}
	for _, f := range formats {
		paths[f] = base + "." + f
	}
	return paths
}

// parseFailOn returns the severity threshold, or "" when the flag is unset.
func parseFailOn(value string) (checks.Severity, error) {
	switch v := checks.Severity(strings.ToLower(strings.TrimSpace(value))); v {
	case "", checks.Critical, checks.High:
		return v, nil
	}
	return "", fmt.Errorf("unknown --fail-on value %q; use critical or high", value)
}

// dedupe trims values and drops blanks and repeats, keeping order.
func dedupe(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func runScan(cmd *cobra.Command, build buildInfo, connect connectFunc, opts scanOptions) error {
	registry := checks.Default()
	formats, err := parseFormats(opts.formats)
	if err != nil {
		return err
	}
	if err := registry.ValidateSkip(opts.skip); err != nil {
		return err
	}
	failOn, err := parseFailOn(opts.failOn)
	if err != nil {
		return err
	}
	namespaces := dedupe(opts.namespaces)

	clients, clusterName, err := connect(opts.kubeContext)
	if err != nil {
		return err
	}
	snap, err := collect.Collect(cmd.Context(), clients, collect.Options{Namespaces: namespaces})
	if err != nil {
		return err
	}
	snap.ClusterName = clusterName

	rep := report.Build(report.Input{
		ToolVersion: build.Version,
		ScanTime:    time.Now(),
		Namespaces:  namespaces,
		Snapshot:    snap,
		Outcomes:    registry.Run(snap, opts.skip),
	})

	written, err := writeReports(rep, build, outputPaths(opts.output, formats), formats)
	if err != nil {
		return err
	}
	if err := report.WriteSummary(cmd.OutOrStdout(), rep, written...); err != nil {
		return err
	}
	// The complete report and summary are out before --fail-on has any
	// effect: it only changes the exit code.
	if failOn != "" && rep.Failed(failOn) {
		return &failOnError{severity: string(failOn)}
	}
	return nil
}

// writeReports renders every requested format before writing any of them, and
// puts each file in place in one step. A failure therefore never leaves a
// half-written report behind, and never one format without the other.
func writeReports(rep report.Report, build buildInfo, paths map[string]string, formats []string) ([]string, error) {
	rendered := map[string][]byte{}
	for _, f := range formats {
		var (
			data []byte
			err  error
		)
		switch f {
		case formatHTML:
			data, err = report.RenderHTML(rep, report.HTMLOptions{CTAURL: build.CTAURL})
		case formatJSON:
			data, err = report.RenderJSON(rep)
		}
		if err != nil {
			return nil, fmt.Errorf("building the %s report: %w", f, err)
		}
		rendered[f] = data
	}
	// Stage every file under a temporary name first. Running out of disk or
	// lacking permission shows up here, before any report exists.
	type stagedFile struct{ tmp, path string }
	var staged []stagedFile
	discard := func() {
		for _, s := range staged {
			_ = os.Remove(s.tmp)
		}
	}
	for _, f := range formats {
		tmp, err := stageFile(paths[f], rendered[f])
		if err != nil {
			discard()
			return nil, fmt.Errorf("writing report: %w", err)
		}
		staged = append(staged, stagedFile{tmp: tmp, path: paths[f]})
	}
	// Then move them into place. If one cannot be moved, take back the ones
	// already moved so the formats never disagree.
	var written []string
	for _, s := range staged {
		if err := os.Rename(s.tmp, s.path); err != nil {
			discard()
			for _, done := range written {
				_ = os.Remove(done)
			}
			return nil, fmt.Errorf("writing report: %w", err)
		}
		written = append(written, s.path)
	}
	return written, nil
}

// stageFile writes data to a temporary file next to path and returns its
// name. Renaming it onto path later replaces the target in one step, so the
// target is only ever the old file or the complete new one.
func stageFile(path string, data []byte) (name string, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".shipready-*.tmp")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err = tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}
