package checks

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

var rel010 = check{
	id: "REL-010",
	meta: Meta{
		Category: Reliability,
		Severity: Medium,
		Title:    "Images using the `latest` tag or no tag",
		Description: "With `latest` you cannot tell which version is running, two replicas can run different code, " +
			"and a rollback has nothing to roll back to. A pod restart can silently pull in an untested build.",
		Remediation: "Deploy a specific version tag or, better, an image digest. Have CI write the tag into the manifest on every release.",
		Snippet:     `image: registry.example.com/api:1.4.2`,
	},
	run: func(s *collect.Snapshot) Result {
		return checkContainers(s, allKinds, true, func(_ workload, c corev1.Container) string {
			return imageProblem(c.Image)
		})
	},
}

// imageProblem returns what is wrong with an image reference's tag, or "".
func imageProblem(image string) string {
	if strings.Contains(image, "@") {
		return "" // pinned by digest
	}
	// Only the last path segment can hold a tag; a colon earlier on is a
	// registry port.
	name := image[strings.LastIndex(image, "/")+1:]
	colon := strings.LastIndex(name, ":")
	switch {
	case colon < 0:
		return "image has no tag, so it resolves to latest"
	case name[colon+1:] == "latest":
		return "image uses the latest tag"
	}
	return ""
}
