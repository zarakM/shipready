package collect

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func deployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func names(ds []appsv1.Deployment) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Namespace+"/"+d.Name)
	}
	return out
}

func TestCollectNamespaceFiltering(t *testing.T) {
	objs := []runtime.Object{
		deployment("shop", "api"),
		deployment("billing", "invoices"),
		deployment("kube-system", "coredns"),
	}
	cases := []struct {
		name       string
		namespaces []string
		want       string
	}{
		{"default excludes system namespaces", nil, "billing/invoices,shop/api"},
		{"filter limits to named namespaces", []string{"shop"}, "shop/api"},
		{"naming a system namespace includes it", []string{"kube-system"}, "kube-system/coredns"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(objs...)
			s, err := Collect(context.Background(), client, Options{Namespaces: tc.namespaces})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(names(s.Deployments), ","); got != tc.want {
				t.Errorf("deployments = %q, want %q", got, tc.want)
			}
			if reason := s.Unavailable(ResDeployments, ResPods, ResPDBs); reason != "" {
				t.Errorf("unexpected unavailable reason: %s", reason)
			}
		})
	}
}

func TestCollectRecordsListErrorsWithoutFailing(t *testing.T) {
	client := fake.NewClientset(deployment("shop", "api"))
	client.PrependReactor("list", ResPDBs, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "policy", Resource: ResPDBs}, "", errors.New("denied"))
	})

	s, err := Collect(context.Background(), client, Options{})
	if err != nil {
		t.Fatalf("a forbidden list must not fail the scan: %v", err)
	}
	if len(s.Deployments) != 1 {
		t.Errorf("deployments = %d, want 1", len(s.Deployments))
	}
	reason := s.Unavailable(ResDeployments, ResPDBs)
	if !strings.Contains(reason, ResPDBs) || !strings.Contains(reason, "permission") {
		t.Errorf("reason = %q, want it to name the resource and the permission problem", reason)
	}
	if s.Unavailable(ResDeployments) != "" {
		t.Errorf("deployments should be available")
	}
}

func TestCollectFailsWhenClusterUnreachable(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "version", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("dial tcp: connection refused")
	})
	if _, err := Collect(context.Background(), client, Options{}); err == nil {
		t.Fatal("expected an error when the cluster cannot be reached")
	}
}

// TestCollectIsReadOnly enforces the hard rule: the collector may only get,
// list and watch.
func TestCollectIsReadOnly(t *testing.T) {
	allowed := map[string]bool{"get": true, "list": true, "watch": true}
	for _, namespaces := range [][]string{nil, {"shop", "billing"}} {
		client := fake.NewClientset(deployment("shop", "api"))
		if _, err := Collect(context.Background(), client, Options{Namespaces: namespaces}); err != nil {
			t.Fatal(err)
		}
		actions := client.Actions()
		if len(actions) == 0 {
			t.Fatal("expected the collector to make API calls")
		}
		for _, a := range actions {
			if !allowed[a.GetVerb()] {
				t.Errorf("collector issued a %q on %s; only get, list and watch are allowed", a.GetVerb(), a.GetResource().Resource)
			}
		}
	}
}

func TestLoadManifestsAndSnapshot(t *testing.T) {
	objs, err := LoadManifests(filepath.Join("..", "..", "testdata", "rel", "bad.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := SnapshotFromObjects(objs)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Deployments) != 1 || len(s.StatefulSets) != 1 || len(s.DaemonSets) != 1 ||
		len(s.CronJobs) != 1 || len(s.Pods) != 3 || len(s.PDBs) != 1 {
		t.Errorf("unexpected fixture contents: %d deployments, %d statefulsets, %d daemonsets, %d cronjobs, %d pods, %d pdbs",
			len(s.Deployments), len(s.StatefulSets), len(s.DaemonSets), len(s.CronJobs), len(s.Pods), len(s.PDBs))
	}
}
