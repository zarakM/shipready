package checks

import (
	"testing"

	"github.com/zarakM/shipready/internal/collect"
)

const (
	relGood = "rel/cluster-good.yaml"
	relBad  = "rel/cluster-bad.yaml"
)

func TestSystemNamespacesAreNotJudged(t *testing.T) {
	// cluster-bad.yaml has a single-replica, latest-tag Deployment in
	// kube-system. On a cluster-wide scan no workload check may report it.
	s := fixture(t, relBad)
	for _, c := range Default().All() {
		for _, r := range c.Run(s).Resources {
			if r.Namespace == "kube-system" {
				t.Errorf("%s reported %s", c.ID(), describe(r))
			}
		}
	}

	// When the user names the namespace, it is judged like any other.
	found := false
	for _, r := range rel010.Run(namespaceLimited(fixture(t, relBad))).Resources {
		found = found || r.Namespace == "kube-system"
	}
	if !found {
		t.Error("kube-system should be judged when it is named with --namespace")
	}
}

func TestREL006SingleReplica(t *testing.T) {
	runCases(t, rel006, []checkCase{
		{name: "replicated workloads", snap: fixture(t, relGood), status: Pass},
		{name: "single replicas, non-production exempt", snap: fixture(t, relBad), status: Fail, want: []string{
			"shop/Deployment/checkout: runs a single replica",
			"shop/Deployment/search: runs a single replica (its autoscaler's minimum is 1)",
			"shop/StatefulSet/queue: runs a single replica",
		}},
		{name: "skipped when autoscalers cannot be listed", snap: deny(fixture(t, relBad), collect.ResHPAs), status: Skipped, skipReason: "horizontalpodautoscalers"},
	})
}

func TestNonProductionNamespaces(t *testing.T) {
	cases := map[string]bool{
		"staging": true, "dev": true, "dev-team-a": true, "checkout-preview": true, "qa": true, "load-test": true,
		"shop": false, "production": false, "devices": false, "contest": false, "default": false, "stagecoach": false,
	}
	for ns, want := range cases {
		if got := nonProduction(ns); got != want {
			t.Errorf("nonProduction(%q) = %v, want %v", ns, got, want)
		}
	}
}

func TestREL007Autoscalers(t *testing.T) {
	runCases(t, rel007, []checkCase{
		{name: "every deployment has an autoscaler", snap: fixture(t, relGood), status: Pass},
		{name: "deployments without one, zero-replica exempt", snap: fixture(t, relBad), status: Fail, want: []string{
			"shop/Deployment/checkout: no autoscaler targets it",
			"staging/Deployment/checkout-preview: no autoscaler targets it",
		}},
		{name: "skipped when autoscalers cannot be listed", snap: deny(fixture(t, relBad), collect.ResHPAs), status: Skipped, skipReason: "horizontalpodautoscalers"},
	})
}

func TestREL008SingleZone(t *testing.T) {
	runCases(t, rel008, []checkCase{
		{name: "nodes in two zones", snap: fixture(t, relGood), status: Pass},
		{name: "all nodes in one zone", snap: fixture(t, relBad), status: Fail, want: []string{
			"/Node/n1: zone us-east-1a",
			"/Node/n2: zone us-east-1a",
		}},
		{name: "skipped when nodes have no zone labels", snap: fixture(t, "cost/bad.yaml"), status: Skipped, skipReason: "zone labels"},
		{name: "skipped with no nodes", snap: snapshot(t), status: Skipped, skipReason: "no nodes"},
		{name: "skipped when nodes cannot be listed", snap: deny(fixture(t, relBad), collect.ResNodes), status: Skipped, skipReason: "nodes"},
	})
}

func TestREL009CertificateExpiry(t *testing.T) {
	runCases(t, rel009, []checkCase{
		{name: "certificates valid for months", snap: fixture(t, relGood), status: Pass},
		{name: "expiring and expired certificates", snap: fixture(t, relBad), status: Fail, want: []string{
			"shop/Certificate/api-tls: expires in 10 days",
			"shop/Certificate/old-tls: expired 6 days ago",
		}},
		{name: "skipped without cert-manager", snap: fixture(t, "rel/good.yaml"), status: Skipped, skipReason: "never opens Secrets"},
		{name: "skipped when certificates cannot be listed", snap: deny(fixture(t, relBad), collect.ResCertificates), status: Skipped, skipReason: "certificates"},
	})
}

func TestREL010ImageTags(t *testing.T) {
	runCases(t, rel010, []checkCase{
		{name: "pinned images", snap: fixture(t, relGood), status: Pass},
		{name: "latest and untagged images, init containers included", snap: fixture(t, relBad), status: Fail, want: []string{
			"shop/Deployment/checkout[checkout]: image uses the latest tag",
			"shop/Deployment/search[search]: image has no tag, so it resolves to latest",
			"staging/Deployment/checkout-preview[setup]: image uses the latest tag",
		}},
		{name: "skipped when pods cannot be listed", snap: deny(fixture(t, relBad), collect.ResPods), status: Skipped, skipReason: "pods"},
	})
}

func TestImageProblem(t *testing.T) {
	cases := []struct {
		image string
		bad   bool
	}{
		{"nginx", true},
		{"nginx:latest", true},
		{"registry.example.com:5000/team/app", true},
		{"registry.example.com:5000/team/app:latest", true},
		{"nginx:1.27", false},
		{"registry.example.com:5000/team/app:v2", false},
		{"nginx@sha256:abc123", false},
		{"nginx:latest@sha256:abc123", false},
	}
	for _, tc := range cases {
		if got := imageProblem(tc.image) != ""; got != tc.bad {
			t.Errorf("imageProblem(%q) flagged = %v, want %v", tc.image, got, tc.bad)
		}
	}
}
