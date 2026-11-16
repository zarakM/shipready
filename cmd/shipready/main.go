// Command shipready scans a Kubernetes cluster, read-only, for
// production-readiness problems.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/zarakM/shipready/internal/collect"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes. A scan that completes exits 0 whatever it found, unless
// --fail-on asks otherwise.
const (
	exitError  = 1 // the scan could not run
	exitFailOn = 2 // the scan ran and --fail-on was tripped
)

// failOnError is returned when the scan completed but found failed checks at
// or above the --fail-on severity.
type failOnError struct{ severity string }

func (e *failOnError) Error() string {
	return fmt.Sprintf("failed checks at %s severity or above were found (--fail-on %s)", e.severity, e.severity)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := newRootCmd(version, collect.Connect).ExecuteContext(ctx)
	if err == nil {
		return
	}
	var failOn *failOnError
	if errors.As(err, &failOn) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitFailOn)
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(exitError)
}
