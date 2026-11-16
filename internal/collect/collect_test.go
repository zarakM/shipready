package collect_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/collect/collecttest"
)

const root = "../.."

func deployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func names(ds []appsv1.Deployment) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Namespace+"/"+d.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestCollectNamespaceScope(t *testing.T) {
	objs := []runtime.Object{
		deployment("shop", "api"),
		deployment("billing", "invoices"),
		deployment("kube-system", "coredns"),
	}
	cases := []struct {
		name        string
		namespaces  []string
		want        string
		clusterWide bool
		systemScope bool // is kube-system judged by workload checks?
	}{
		{"whole cluster keeps system namespaces but out of scope", nil, "billing/invoices,kube-system/coredns,shop/api", true, false},
		{"filter limits to named namespaces", []string{"shop"}, "shop/api", false, true},
		{"naming a system namespace puts it in scope", []string{"kube-system"}, "kube-system/coredns", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := collecttest.NewCluster(t, objs...)
			s, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{Namespaces: tc.namespaces})
			if err != nil {
				t.Fatal(err)
			}
			if got := names(s.Deployments); got != tc.want {
				t.Errorf("deployments = %q, want %q", got, tc.want)
			}
			if s.ClusterWide != tc.clusterWide {
				t.Errorf("ClusterWide = %v, want %v", s.ClusterWide, tc.clusterWide)
			}
			if got := s.InScope("kube-system"); got != tc.systemScope {
				t.Errorf("InScope(kube-system) = %v, want %v", got, tc.systemScope)
			}
			if !s.InScope("shop") {
				t.Error("ordinary namespaces are always in scope")
			}
			if len(s.Errors) != 0 {
				t.Errorf("unexpected collection errors: %v", s.Errors)
			}
			if s.CollectedAt.IsZero() {
				t.Error("CollectedAt must be set")
			}
		})
	}
}

func TestCollectRecordsListErrorsWithoutFailing(t *testing.T) {
	cluster := collecttest.NewCluster(t, deployment("shop", "api"))
	cluster.Kube.PrependReactor("list", collect.ResPDBs, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "policy", Resource: collect.ResPDBs}, "", errors.New("denied"))
	})

	s, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{})
	if err != nil {
		t.Fatalf("a forbidden list must not fail the scan: %v", err)
	}
	if len(s.Deployments) != 1 {
		t.Errorf("deployments = %d, want 1", len(s.Deployments))
	}
	reason := s.Unavailable(collect.ResDeployments, collect.ResPDBs)
	if !strings.Contains(reason, collect.ResPDBs) || !strings.Contains(reason, "permission") {
		t.Errorf("reason = %q, want it to name the resource and the permission problem", reason)
	}
	if s.Unavailable(collect.ResDeployments) != "" {
		t.Errorf("deployments should be available")
	}
}

func TestCollectFailsWhenClusterUnreachable(t *testing.T) {
	cluster := collecttest.NewCluster(t)
	cluster.Kube.PrependReactor("get", "version", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("dial tcp: connection refused")
	})
	if _, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{}); err == nil {
		t.Fatal("expected an error when the cluster cannot be reached")
	}
}

func TestCollectAddons(t *testing.T) {
	objs := collecttest.Load(t, root, "rel/cluster-bad.yaml", "cost/bad.yaml", "rec/bad.yaml")

	t.Run("installed add-ons are read", func(t *testing.T) {
		cluster := collecttest.NewCluster(t, objs...)
		s, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Errors) != 0 {
			t.Fatalf("unexpected collection errors: %v", s.Errors)
		}
		for _, g := range []string{collect.GroupMetrics, collect.GroupCertManager, collect.GroupVelero, collect.GroupSnapshots} {
			if !s.HasAPI(g) {
				t.Errorf("API group %s should be detected", g)
			}
		}
		if s.HasAPI(collect.GroupKarpenter) {
			t.Error("Karpenter is not installed in the fixtures")
		}

		// The collected data must match what the fixtures decode to directly.
		want, err := collect.SnapshotFromObjects(objs)
		if err != nil {
			t.Fatal(err)
		}
		counts := []struct {
			name      string
			got, want int
		}{
			{"pod metrics", len(s.PodMetrics), len(want.PodMetrics)},
			{"certificates", len(s.Certificates), len(want.Certificates)},
			{"velero backups", len(s.VeleroBackups), len(want.VeleroBackups)},
			{"velero schedules", len(s.VeleroSchedules), len(want.VeleroSchedules)},
			{"volume snapshots", len(s.VolumeSnapshots), len(want.VolumeSnapshots)},
			{"nodes", len(s.Nodes), len(want.Nodes)},
			{"hpas", len(s.HPAs), len(want.HPAs)},
			{"persistent volumes", len(s.PVs), len(want.PVs)},
		}
		for _, c := range counts {
			if c.got != c.want || c.want == 0 {
				t.Errorf("%s = %d, want %d (and more than zero)", c.name, c.got, c.want)
			}
		}

		var api collect.PodUsage
		for _, u := range s.PodMetrics {
			if u.Name == "api-1" {
				api = u
			}
		}
		if api.CPUMilli != 150 || api.MemoryBytes != 1500<<20 {
			t.Errorf("api-1 usage = %dm CPU, %d bytes; want 150m and 1500Mi", api.CPUMilli, api.MemoryBytes)
		}
		for _, c := range s.Certificates {
			if c.Name == "api-tls" && !c.NotAfter.Equal(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)) {
				t.Errorf("api-tls NotAfter = %v", c.NotAfter)
			}
		}
	})

	t.Run("add-ons that are not installed are never queried", func(t *testing.T) {
		typed, _ := collect.SplitObjects(objs)
		cluster := collecttest.NewCluster(t, typed...)
		s, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if n := len(cluster.Dynamic.Actions()); n != 0 {
			t.Errorf("made %d add-on API calls on a cluster with no add-ons", n)
		}
		if len(s.Errors) != 0 {
			t.Errorf("missing add-ons are not errors: %v", s.Errors)
		}
	})

	t.Run("an add-on that cannot be listed is recorded", func(t *testing.T) {
		cluster := collecttest.NewCluster(t, objs...)
		cluster.Dynamic.PrependReactor("list", "backups", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: collect.GroupVelero, Resource: "backups"}, "", errors.New("denied"))
		})
		s, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if s.Unavailable(collect.ResVeleroBackups) == "" {
			t.Error("Velero backups should be recorded as unavailable")
		}
		if s.Unavailable(collect.ResVeleroSchedules) != "" {
			t.Error("Velero schedules should still be available")
		}
	})
}

// TestCollectIsReadOnly enforces two hard rules: the collector may only get,
// list and watch, and it never touches Secrets.
func TestCollectIsReadOnly(t *testing.T) {
	allowed := map[string]bool{"get": true, "list": true, "watch": true}
	objs := collecttest.Load(t, root, "rel/cluster-bad.yaml", "cost/bad.yaml", "rec/bad.yaml", "sec/bad.yaml")

	for _, namespaces := range [][]string{nil, {"shop", "billing"}} {
		cluster := collecttest.NewCluster(t, objs...)
		if _, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{Namespaces: namespaces}); err != nil {
			t.Fatal(err)
		}
		actions := append(cluster.Kube.Actions(), cluster.Dynamic.Actions()...)
		if len(cluster.Kube.Actions()) == 0 || len(cluster.Dynamic.Actions()) == 0 {
			t.Fatal("expected the collector to call both the core and the add-on APIs")
		}
		for _, a := range actions {
			if !allowed[a.GetVerb()] {
				t.Errorf("collector issued a %q on %s; only get, list and watch are allowed", a.GetVerb(), a.GetResource().Resource)
			}
			if a.GetResource().Resource == "secrets" {
				t.Errorf("collector issued a %q on secrets; it must never read them", a.GetVerb())
			}
		}
	}
}

func TestLoadManifestsAndSnapshot(t *testing.T) {
	s, err := collect.SnapshotFromObjects(collecttest.Load(t, root, "rel/bad.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Deployments) != 1 || len(s.StatefulSets) != 1 || len(s.DaemonSets) != 1 ||
		len(s.CronJobs) != 1 || len(s.Pods) != 3 || len(s.PDBs) != 1 {
		t.Errorf("unexpected fixture contents: %d deployments, %d statefulsets, %d daemonsets, %d cronjobs, %d pods, %d pdbs",
			len(s.Deployments), len(s.StatefulSets), len(s.DaemonSets), len(s.CronJobs), len(s.Pods), len(s.PDBs))
	}
	if !s.ClusterWide {
		t.Error("a snapshot built from objects is cluster-wide")
	}
}
