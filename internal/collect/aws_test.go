package collect_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/collect/collecttest"
)

func collectAWS(account *collecttest.FakeAWS, cluster string) *collect.AWSSnapshot {
	return collect.CollectAWS(context.Background(), account.Clients("us-east-1", cluster))
}

func TestCollectAWS(t *testing.T) {
	account := collecttest.ProblemAccount()
	a := collectAWS(account, "prod")

	if len(a.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", a.Errors)
	}
	if a.Region != "us-east-1" || a.ClusterName != "prod" {
		t.Errorf("region %q, cluster %q", a.Region, a.ClusterName)
	}

	wantCluster := &collect.EKSCluster{
		Name: "prod", Version: "1.30", VPCID: collecttest.ClusterVPC,
		EndpointPublic: true, PublicCIDRs: []string{"0.0.0.0/0"},
		EnabledLogs:         []string{"api"}, // the disabled log setup is ignored
		VersionStatus:       collect.VersionExtendedSupport,
		StandardSupportEnds: time.Date(2025, 7, 23, 0, 0, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(a.Cluster, wantCluster) {
		t.Errorf("cluster:\n got  %+v\n want %+v", a.Cluster, wantCluster)
	}

	wantGroups := []collect.EKSNodeGroup{
		{Name: "general", Zones: []string{"us-east-1a"}}, // two subnets, one zone
		{Name: "spot", Zones: []string{"us-east-1a", "us-east-1b"}},
	}
	if !reflect.DeepEqual(a.NodeGroups, wantGroups) {
		t.Errorf("node groups = %+v, want %+v", a.NodeGroups, wantGroups)
	}

	// Only unattached volumes, from anywhere in the region.
	wantVolumes := []collect.EBSVolume{
		{ID: "vol-0aaa111", Name: "old-postgres-data", SizeGiB: 200, Type: "gp3"},
		{ID: "vol-0bbb222", SizeGiB: 20, Type: "gp3"},
		{ID: "vol-0ddd444", SizeGiB: 50, Type: "gp3"},
	}
	if !reflect.DeepEqual(a.Volumes, wantVolumes) {
		t.Errorf("volumes = %+v, want %+v", a.Volumes, wantVolumes)
	}

	// Only ALBs and NLBs in the cluster's VPC, with target health summed
	// across target groups.
	wantLBs := []collect.LoadBalancer{
		{Name: "web", Type: "application", TargetGroups: 2, RegisteredTargets: 3, HealthyTargets: 2},
		{Name: "old-demo", Type: "application"},
		{Name: "admin", Type: "network", TargetGroups: 1, RegisteredTargets: 2},
	}
	if !reflect.DeepEqual(a.LoadBalancers, wantLBs) {
		t.Errorf("load balancers = %+v, want %+v", a.LoadBalancers, wantLBs)
	}

	// Only databases in the cluster's VPC. The read replica is left out, and
	// the Aurora member is represented by its cluster.
	wantDBs := []collect.Database{
		{ID: "analytics", Engine: "aurora-postgresql", Cluster: true, RetentionDays: 1},
		{ID: "orders", Engine: "postgres", RetentionDays: 0},
		{ID: "users", Engine: "postgres", RetentionDays: 14},
	}
	if !reflect.DeepEqual(a.Databases, wantDBs) {
		t.Errorf("databases = %+v, want %+v", a.Databases, wantDBs)
	}
}

func TestCollectAWSPagination(t *testing.T) {
	account := collecttest.ProblemAccount()
	account.PageSize = 1
	a := collectAWS(account, "prod")
	if len(a.Volumes) != 3 {
		t.Errorf("volumes = %d, want all 3 across pages", len(a.Volumes))
	}
	pages := 0
	for _, call := range account.Calls() {
		if call == "DescribeVolumes" {
			pages++
		}
	}
	if pages != 3 {
		t.Errorf("DescribeVolumes was called %d times, want 3 pages", pages)
	}
}

func TestCollectAWSRecordsErrorsWithoutFailing(t *testing.T) {
	cases := []struct {
		name        string
		cluster     string
		fail        string
		unavailable []string
		available   []string
		reason      string
	}{
		{
			name: "one service denied leaves the others", cluster: "prod", fail: "DescribeDBInstances",
			unavailable: []string{collect.ResRDS},
			available:   []string{collect.ResEKSCluster, collect.ResEKSVersions, collect.ResEKSNodeGroups, collect.ResEBSVolumes, collect.ResLoadBalancers},
			reason:      "permission",
		},
		{
			name: "target health denied", cluster: "prod", fail: "DescribeTargetHealth",
			unavailable: []string{collect.ResLoadBalancers},
			available:   []string{collect.ResRDS, collect.ResEBSVolumes},
			reason:      "permission",
		},
		{
			name: "version support denied still leaves the cluster", cluster: "prod", fail: "DescribeClusterVersions",
			unavailable: []string{collect.ResEKSVersions},
			available:   []string{collect.ResEKSCluster, collect.ResEKSNodeGroups},
			reason:      "permission",
		},
		{
			name: "subnets denied loses the node group zones", cluster: "prod", fail: "DescribeSubnets",
			unavailable: []string{collect.ResEKSNodeGroups},
			available:   []string{collect.ResEKSCluster, collect.ResLoadBalancers},
			reason:      "permission",
		},
		{
			// Without the cluster there is no VPC to scope to, but volumes
			// are region-wide and still read.
			name: "cluster denied", cluster: "prod", fail: "DescribeCluster",
			unavailable: []string{collect.ResEKSCluster, collect.ResEKSVersions, collect.ResEKSNodeGroups, collect.ResLoadBalancers, collect.ResRDS},
			available:   []string{collect.ResEBSVolumes},
			reason:      "permission",
		},
		{
			name: "cluster name unknown", cluster: "",
			unavailable: []string{collect.ResEKSCluster, collect.ResEKSNodeGroups, collect.ResLoadBalancers, collect.ResRDS},
			available:   []string{collect.ResEBSVolumes},
			reason:      "--eks-cluster",
		},
		{
			name: "cluster not in this account or region", cluster: "staging",
			unavailable: []string{collect.ResEKSCluster, collect.ResRDS},
			available:   []string{collect.ResEBSVolumes},
			reason:      "No cluster found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := collecttest.ProblemAccount()
			if tc.fail != "" {
				account.Fail = map[string]error{tc.fail: collecttest.AccessDenied(tc.fail)}
			}
			a := collectAWS(account, tc.cluster)
			for _, res := range tc.unavailable {
				if a.Unavailable(res) == "" {
					t.Errorf("%s should be unavailable", res)
				}
			}
			for _, res := range tc.available {
				if reason := a.Unavailable(res); reason != "" {
					t.Errorf("%s should be available, got: %s", res, reason)
				}
			}
			if reason := a.Unavailable(tc.unavailable[0]); !strings.Contains(reason, tc.reason) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.reason)
			}
			if len(a.Volumes) != 3 {
				t.Errorf("volumes = %d, want 3: region-wide data should survive", len(a.Volumes))
			}
		})
	}
}

// The hard rule for AWS: only Describe, List and Get calls. The collector can
// only call what these interfaces expose, so checking their method names
// covers every call it could ever make.
func TestAWSInterfacesAreReadOnly(t *testing.T) {
	interfaces := []reflect.Type{
		reflect.TypeOf((*collect.EKSAPI)(nil)).Elem(),
		reflect.TypeOf((*collect.EC2API)(nil)).Elem(),
		reflect.TypeOf((*collect.ELBAPI)(nil)).Elem(),
		reflect.TypeOf((*collect.RDSAPI)(nil)).Elem(),
	}
	total := 0
	for _, iface := range interfaces {
		for i := 0; i < iface.NumMethod(); i++ {
			name := iface.Method(i).Name
			total++
			if !strings.HasPrefix(name, "Describe") && !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
				t.Errorf("%s.%s is not a read-only call; only Describe*, List* and Get* are allowed", iface.Name(), name)
			}
		}
	}
	if total == 0 {
		t.Fatal("no interface methods found")
	}

	// And the calls actually made during a full collection.
	account := collecttest.ProblemAccount()
	collectAWS(account, "prod")
	if len(account.Calls()) == 0 {
		t.Fatal("expected AWS calls")
	}
	for _, call := range account.Calls() {
		if !strings.HasPrefix(call, "Describe") && !strings.HasPrefix(call, "List") && !strings.HasPrefix(call, "Get") {
			t.Errorf("collector called %s", call)
		}
	}
}

func TestAWSDataReasons(t *testing.T) {
	s := collect.NewSnapshot()
	if a, reason := s.AWSData(); a != nil || !strings.Contains(reason, "--aws") {
		t.Errorf("without --aws: data %v, reason %q", a, reason)
	}
	s.AWSSkipReason = "no usable AWS credentials were found"
	if _, reason := s.AWSData(); reason != s.AWSSkipReason {
		t.Errorf("reason = %q, want the recorded one", reason)
	}
	s.AWS = &collect.AWSSnapshot{Errors: map[string]error{collect.ResRDS: errors.New("boom")}}
	if a, reason := s.AWSData(); a == nil || reason != "" {
		t.Errorf("with AWS data: data %v, reason %q", a, reason)
	}
}

func TestDetectEKS(t *testing.T) {
	awsExec := func(args ...string) *clientcmdapi.ExecConfig {
		return &clientcmdapi.ExecConfig{Command: "aws", Args: args}
	}
	cases := []struct {
		name    string
		entry   string
		server  string
		exec    *clientcmdapi.ExecConfig
		cluster string
		region  string
	}{
		{
			name:    "aws eks update-kubeconfig",
			entry:   "arn:aws:eks:us-east-1:123456789012:cluster/prod",
			server:  "https://ABCDEF0123.gr7.us-east-1.eks.amazonaws.com",
			exec:    awsExec("--region", "us-east-1", "eks", "get-token", "--cluster-name", "prod", "--output", "json"),
			cluster: "prod", region: "us-east-1",
		},
		{
			name:    "GovCloud partition",
			entry:   "arn:aws-us-gov:eks:us-gov-west-1:123456789012:cluster/secure",
			cluster: "secure", region: "us-gov-west-1",
		},
		{
			name:    "eksctl",
			entry:   "prod.eu-west-2.eksctl.io",
			cluster: "prod", region: "eu-west-2",
		},
		{
			name:    "renamed context, details from the token command",
			entry:   "production",
			server:  "https://ABCDEF0123.sk1.ap-southeast-2.eks.amazonaws.com",
			exec:    awsExec("eks", "get-token", "--cluster-name=payments", "--region=ap-southeast-2"),
			cluster: "payments", region: "ap-southeast-2",
		},
		{
			name:    "aws-iam-authenticator with region from the endpoint",
			entry:   "production",
			server:  "https://ABCDEF0123.yl4.eu-central-1.eks.amazonaws.com",
			exec:    &clientcmdapi.ExecConfig{Command: "aws-iam-authenticator", Args: []string{"token", "-i", "legacy"}},
			cluster: "legacy", region: "eu-central-1",
		},
		{
			name:    "region from the token command's environment",
			entry:   "production",
			exec:    &clientcmdapi.ExecConfig{Command: "aws", Args: []string{"eks", "get-token", "--cluster-name", "prod"}, Env: []clientcmdapi.ExecEnvVar{{Name: "AWS_REGION", Value: "ca-central-1"}}},
			cluster: "prod", region: "ca-central-1",
		},
		{
			name:   "endpoint only gives the region",
			entry:  "production",
			server: "https://ABCDEF0123.gr7.us-west-2.eks.amazonaws.com",
			region: "us-west-2",
		},
		{
			name:   "not EKS",
			entry:  "kind-local",
			server: "https://127.0.0.1:6443",
		},
		{
			name:  "a flag with no value does not crash",
			entry: "production",
			exec:  awsExec("eks", "get-token", "--cluster-name"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collect.DetectEKS(tc.entry, tc.server, tc.exec)
			if got.Cluster != tc.cluster || got.Region != tc.region {
				t.Errorf("got cluster %q region %q, want %q %q", got.Cluster, got.Region, tc.cluster, tc.region)
			}
		})
	}
}
