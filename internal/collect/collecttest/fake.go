// Package collecttest builds fake clusters for tests. It is only imported by
// test files, so the fake clients never reach the shipped binary.
package collecttest

import (
	"path/filepath"
	"sort"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/zarakM/shipready/internal/collect"
)

// Cluster is a fake cluster. Kube and Dynamic record every call made to them.
type Cluster struct {
	Kube    *fake.Clientset
	Dynamic *dynamicfake.FakeDynamicClient
}

// Clients returns the fakes as the collector's client set.
func (c *Cluster) Clients() collect.Clients {
	return collect.Clients{Kube: c.Kube, Dynamic: c.Dynamic}
}

// Load reads fixture files relative to the repository's testdata directory.
// root is the path from the calling test's package to the repository root.
func Load(t testing.TB, root string, fixtures ...string) []runtime.Object {
	t.Helper()
	var objs []runtime.Object
	for _, name := range fixtures {
		loaded, err := collect.LoadManifests(filepath.Join(root, "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		objs = append(objs, loaded...)
	}
	return objs
}

// NewCluster serves the given objects. Built-in kinds go to the fake
// clientset; add-on kinds go to the fake dynamic client, and their API groups
// are advertised through discovery as installed.
func NewCluster(t testing.TB, objs ...runtime.Object) *Cluster {
	t.Helper()
	typed, addons := collect.SplitObjects(objs)

	kube := fake.NewClientset(typed...)
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), collect.DynamicListKinds())

	groupVersions := map[string]bool{}
	for _, u := range addons {
		gvk := u.GroupVersionKind()
		gvr, ok := collect.DynamicResource(gvk.GroupKind())
		if !ok {
			t.Fatalf("fixture has unsupported kind %s", gvk)
		}
		if err := dyn.Tracker().Create(gvr, u, u.GetNamespace()); err != nil {
			t.Fatal(err)
		}
		groupVersions[gvk.GroupVersion().String()] = true
	}
	c := &Cluster{Kube: kube, Dynamic: dyn}
	for gv := range groupVersions {
		c.Install(gv)
	}
	return c
}

// Install advertises an API group version through discovery.
func (c *Cluster) Install(groupVersion string) {
	c.Kube.Resources = append(c.Kube.Resources, &metav1.APIResourceList{GroupVersion: groupVersion})
	sort.Slice(c.Kube.Resources, func(i, j int) bool {
		return c.Kube.Resources[i].GroupVersion < c.Kube.Resources[j].GroupVersion
	})
}
