package collect_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/collect/collecttest"
)

// These tests tie the permission files users apply to what the scanner really
// does. If the collector starts reading something new, or stops reading
// something, the files must change with it.

// TestClusterRoleMatchesCollector checks deploy/kubernetes/clusterrole.yaml
// against the API calls of a full collection with every add-on installed.
func TestClusterRoleMatchesCollector(t *testing.T) {
	var role *rbacv1.ClusterRole
	var binding *rbacv1.ClusterRoleBinding
	manifest, err := collect.LoadManifests(filepath.Join(root, "deploy", "kubernetes", "clusterrole.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, obj := range manifest {
		switch o := obj.(type) {
		case *rbacv1.ClusterRole:
			role = o
		case *rbacv1.ClusterRoleBinding:
			binding = o
		}
	}
	if role == nil || binding == nil {
		t.Fatal("the manifest must hold a ClusterRole and a ClusterRoleBinding")
	}
	if binding.RoleRef.Name != role.Name || len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "Group" {
		t.Errorf("the binding should give the role to exactly one group, got %+v -> %+v", binding.Subjects, binding.RoleRef)
	}

	granted := map[string]bool{}
	for _, rule := range role.Rules {
		if !reflect.DeepEqual(rule.Verbs, []string{"list"}) {
			t.Errorf("rule for %v grants %v; the scanner only needs list", rule.Resources, rule.Verbs)
		}
		if len(rule.NonResourceURLs) > 0 || len(rule.ResourceNames) > 0 {
			t.Errorf("rule for %v should be a plain resource rule", rule.Resources)
		}
		for _, group := range rule.APIGroups {
			for _, res := range rule.Resources {
				if group == "*" || res == "*" {
					t.Errorf("wildcard in rule %v/%v", rule.APIGroups, rule.Resources)
				}
				if res == "secrets" {
					t.Error("the role must never grant access to secrets")
				}
				granted[group+"/"+res] = true
			}
		}
	}

	// Everything the collector lists, on a cluster with every add-on, both
	// for a whole-cluster scan and a namespace-limited one.
	objs := collecttest.Load(t, root, "rel/cluster-bad.yaml", "cost/bad.yaml", "rec/bad.yaml", "sec/bad.yaml")
	used := map[string]bool{}
	for _, namespaces := range [][]string{nil, {"shop"}} {
		cluster := collecttest.NewCluster(t, objs...)
		s, err := collect.Collect(context.Background(), cluster.Clients(), collect.Options{Namespaces: namespaces})
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Errors) != 0 {
			t.Fatalf("collection errors: %v", s.Errors)
		}
		for _, a := range append(cluster.Kube.Actions(), cluster.Dynamic.Actions()...) {
			res := a.GetResource()
			switch a.GetVerb() {
			case "list":
				used[res.Group+"/"+res.Resource] = true
			case "get":
				// API discovery (/version, /apis), which every signed-in
				// user may read without any role.
				if res.Resource != "version" && res.Resource != "group" {
					t.Errorf("unexpected get on %s", res.Resource)
				}
			default:
				t.Errorf("unexpected %s on %s", a.GetVerb(), res.Resource)
			}
		}
	}

	if got, want := keys(granted), keys(used); !reflect.DeepEqual(got, want) {
		t.Errorf("the ClusterRole and the collector disagree.\n role grants:    %v\n collector uses: %v\n only in role:      %v\n missing from role: %v",
			got, want, minus(granted, used), minus(used, granted))
	}
}

// TestIAMPolicyMatchesCollector checks deploy/aws/iam-policy.json against the
// AWS client interfaces, which are the only AWS calls the collector can make.
func TestIAMPolicyMatchesCollector(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "deploy", "aws", "iam-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Version   string
		Statement []struct {
			Sid      string
			Effect   string
			Action   []string
			Resource string
		}
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&policy); err != nil {
		t.Fatalf("iam-policy.json: %v", err)
	}
	if policy.Version != "2012-10-17" || len(policy.Statement) != 1 || policy.Statement[0].Effect != "Allow" {
		t.Fatalf("expected one Allow statement in a 2012-10-17 policy, got %+v", policy)
	}

	granted := map[string]bool{}
	for _, action := range policy.Statement[0].Action {
		if strings.Contains(action, "*") {
			t.Errorf("wildcard action %q", action)
		}
		granted[action] = true
	}
	if !sort.StringsAreSorted(policy.Statement[0].Action) {
		t.Error("keep the actions sorted so the file is easy to review")
	}

	services := map[string]reflect.Type{
		"eks":                  reflect.TypeOf((*collect.EKSAPI)(nil)).Elem(),
		"ec2":                  reflect.TypeOf((*collect.EC2API)(nil)).Elem(),
		"elasticloadbalancing": reflect.TypeOf((*collect.ELBAPI)(nil)).Elem(),
		"rds":                  reflect.TypeOf((*collect.RDSAPI)(nil)).Elem(),
	}
	used := map[string]bool{}
	for prefix, iface := range services {
		for i := 0; i < iface.NumMethod(); i++ {
			used[prefix+":"+iface.Method(i).Name] = true
		}
	}
	if got, want := keys(granted), keys(used); !reflect.DeepEqual(got, want) {
		t.Errorf("the IAM policy and the collector disagree.\n only in policy:      %v\n missing from policy: %v", minus(granted, used), minus(used, granted))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func minus(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
