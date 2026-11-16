// Command shipready scans a Kubernetes cluster, read-only, for
// production-readiness problems.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/zarakM/shipready/internal/collect"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := newRootCmd(version, collect.Connect).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
