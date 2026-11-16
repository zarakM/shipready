package checks

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
)

// fixture loads testdata/<name> into a snapshot.
func fixture(t *testing.T, name string) *collect.Snapshot {
	t.Helper()
	objs, err := collect.LoadManifests(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot(t, objs...)
}

func snapshot(t *testing.T, objs ...runtime.Object) *collect.Snapshot {
	t.Helper()
	s, err := collect.SnapshotFromObjects(objs)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// forbidden returns the good fixture with one resource type made unreadable.
func forbidden(t *testing.T, resource string) *collect.Snapshot {
	t.Helper()
	s := fixture(t, "rel/good.yaml")
	s.Errors[resource] = apierrors.NewForbidden(schema.GroupResource{Resource: resource}, "", errors.New("denied"))
	return s
}

// describe renders a finding as "namespace/Kind/name[container]: detail".
func describe(r Resource) string {
	out := r.Namespace + "/" + r.Kind + "/" + r.Name
	if r.Container != "" {
		out += "[" + r.Container + "]"
	}
	return out + ": " + r.Detail
}

type checkCase struct {
	name       string
	snap       *collect.Snapshot
	status     Status
	want       []string // describe() of each affected resource, in order
	skipReason string   // substring expected in the skip reason
}

func runCases(t *testing.T, c Check, cases []checkCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.Run(tc.snap)
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s (resources: %v, reason: %q)", got.Status, tc.status, got.Resources, got.SkipReason)
			}
			var have []string
			for _, r := range got.Resources {
				have = append(have, describe(r))
			}
			if !reflect.DeepEqual(have, tc.want) {
				t.Errorf("affected resources:\n got  %s\n want %s", strings.Join(have, "\n      "), strings.Join(tc.want, "\n      "))
			}
			if tc.status == Skipped && (got.SkipReason == "" || !strings.Contains(got.SkipReason, tc.skipReason)) {
				t.Errorf("skip reason = %q, want it to contain %q", got.SkipReason, tc.skipReason)
			}
		})
	}
}
