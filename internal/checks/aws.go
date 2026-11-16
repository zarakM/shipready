package checks

import (
	"fmt"

	"github.com/zarakM/shipready/internal/collect"
)

// awsData returns the AWS side of the scan for a check that needs the given
// resource types. The second value is the skip reason when the check cannot
// run: --aws was not given, AWS could not be reached, or a resource could not
// be read.
func awsData(s *collect.Snapshot, resources ...string) (*collect.AWSSnapshot, string) {
	a, reason := s.AWSData()
	if a == nil {
		return nil, reason
	}
	if reason := a.Unavailable(resources...); reason != "" {
		return nil, reason
	}
	return a, ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
