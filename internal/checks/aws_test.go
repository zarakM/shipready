package checks

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/zarakM/shipready/internal/collect"
)

// awsFixture returns a snapshot whose AWS side comes from testdata/aws/<name>.
func awsFixture(t *testing.T, name string) *collect.Snapshot {
	t.Helper()
	a, err := collect.LoadAWSFixture(filepath.Join("..", "..", "testdata", "aws", name))
	if err != nil {
		t.Fatal(err)
	}
	s := snapshot(t)
	s.AWS = a
	return s
}

// awsDeny marks AWS resource types as unreadable.
func awsDeny(s *collect.Snapshot, resources ...string) *collect.Snapshot {
	for _, r := range resources {
		s.AWS.Errors[r] = errDenied(r)
	}
	return s
}

func awsWith(s *collect.Snapshot, change func(*collect.AWSSnapshot)) *collect.Snapshot {
	change(s.AWS)
	return s
}

// Every AWS check is skipped, with the reason, when --aws was not given or
// when AWS could not be reached.
func TestAWSChecksWithoutAWS(t *testing.T) {
	unreachable := snapshot(t)
	unreachable.AWSSkipReason = "no usable AWS credentials were found"

	n := 0
	for _, c := range Default().All() {
		if c.ID()[:4] != "AWS-" {
			continue
		}
		n++
		runCases(t, c, []checkCase{
			{name: c.ID() + " not requested", snap: snapshot(t), status: Skipped, skipReason: "run with --aws"},
			{name: c.ID() + " AWS unreachable", snap: unreachable, status: Skipped, skipReason: "no usable AWS credentials"},
		})
	}
	if n != 7 {
		t.Errorf("found %d AWS checks, want 7", n)
	}
}

func TestAWS001PublicEndpoint(t *testing.T) {
	runCases(t, aws001, []checkCase{
		{name: "public but restricted to known ranges", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "open to the internet", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			"/EKS cluster/prod: API endpoint accepts connections from any IP address",
		}},
		{
			name:   "an empty range list means everyone",
			snap:   awsWith(awsFixture(t, "good.json"), func(a *collect.AWSSnapshot) { a.Cluster.PublicCIDRs = nil }),
			status: Fail,
			want:   []string{"/EKS cluster/prod: API endpoint accepts connections from any IP address"},
		},
		{
			name:   "private endpoint only",
			snap:   awsWith(awsFixture(t, "bad.json"), func(a *collect.AWSSnapshot) { a.Cluster.EndpointPublic = false }),
			status: Pass,
		},
		{name: "skipped when the cluster cannot be read", snap: awsDeny(awsFixture(t, "bad.json"), collect.ResEKSCluster), status: Skipped, skipReason: "the EKS cluster"},
	})
}

func TestAWS002ControlPlaneLogs(t *testing.T) {
	runCases(t, aws002, []checkCase{
		{name: "api, audit and authenticator on", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "audit and authenticator off", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			"/EKS cluster/prod: not logging: audit, authenticator",
		}},
		{
			name:   "nothing logged",
			snap:   awsWith(awsFixture(t, "bad.json"), func(a *collect.AWSSnapshot) { a.Cluster.EnabledLogs = nil }),
			status: Fail,
			want:   []string{"/EKS cluster/prod: not logging: api, audit, authenticator"},
		},
		{name: "skipped when the cluster cannot be read", snap: awsDeny(awsFixture(t, "bad.json"), collect.ResEKSCluster), status: Skipped, skipReason: "the EKS cluster"},
	})
}

func TestAWS003UnattachedVolumes(t *testing.T) {
	runCases(t, aws003, []checkCase{
		{name: "no unattached volumes", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "unattached volumes", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			`/EBS volume/vol-0aaa111: 200 GiB gp3, attached to nothing (named "old-postgres-data")`,
			"/EBS volume/vol-0bbb222: 20 GiB gp2, attached to nothing",
		}},
		{name: "skipped when volumes cannot be read", snap: awsDeny(awsFixture(t, "bad.json"), collect.ResEBSVolumes), status: Skipped, skipReason: "EBS volumes"},
		{name: "not skipped when only the cluster is unreadable", snap: awsDeny(awsFixture(t, "good.json"), collect.ResEKSCluster), status: Pass},
	})
}

func TestAWS004IdleLoadBalancers(t *testing.T) {
	runCases(t, aws004, []checkCase{
		{name: "one unhealthy target among healthy ones is fine", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "nothing healthy behind them", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			"/Application Load Balancer/k8s-old-demo-9f8e: no target groups",
			"/Network Load Balancer/k8s-shop-admin-5e4f: 2 targets registered, none healthy",
			"/Network Load Balancer/k8s-shop-grpc-7c6d: no targets registered",
		}},
		{name: "skipped when load balancers cannot be read", snap: awsDeny(awsFixture(t, "bad.json"), collect.ResLoadBalancers), status: Skipped, skipReason: "load balancers"},
	})
}

func TestAWS005DatabaseBackups(t *testing.T) {
	runCases(t, aws005, []checkCase{
		{name: "seven days or more", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "backups off or kept too briefly", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			"/RDS cluster/analytics: backups are kept for only 1 day (aurora-postgresql)",
			"/RDS instance/orders: automated backups are off (postgres)",
			"/RDS instance/sessions: backups are kept for only 3 days (mysql)",
		}},
		{name: "skipped when databases cannot be read", snap: awsDeny(awsFixture(t, "bad.json"), collect.ResRDS), status: Skipped, skipReason: "RDS databases"},
	})
}

func TestAWS006NodeGroupZones(t *testing.T) {
	runCases(t, aws006, []checkCase{
		{name: "groups span several zones", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "one group in a single zone", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			"/Node group/general: all subnets are in us-east-1a",
		}},
		{
			name:   "skipped when there are no managed node groups",
			snap:   awsWith(awsFixture(t, "bad.json"), func(a *collect.AWSSnapshot) { a.NodeGroups = nil }),
			status: Skipped, skipReason: "no managed node groups",
		},
		{name: "skipped when node groups cannot be read", snap: awsDeny(awsFixture(t, "bad.json"), collect.ResEKSNodeGroups), status: Skipped, skipReason: "EKS node groups"},
	})
}

func TestAWS007VersionSupport(t *testing.T) {
	runCases(t, aws007, []checkCase{
		{name: "in standard support", snap: awsFixture(t, "good.json"), status: Pass},
		{name: "in extended support", snap: awsFixture(t, "bad.json"), status: Fail, want: []string{
			"/EKS cluster/prod: runs Kubernetes 1.30; standard support ended on 23 Jul 2025",
		}},
		{
			// The date decides even if AWS still reports the old status.
			name: "past the end date",
			snap: awsWith(awsFixture(t, "good.json"), func(a *collect.AWSSnapshot) {
				a.Cluster.StandardSupportEnds = testNow.Add(-24 * time.Hour)
			}),
			status: Fail,
			want:   []string{"/EKS cluster/prod: runs Kubernetes 1.34; standard support ended on 15 Nov 2026"},
		},
		{
			name:   "status alone is enough when no date is given",
			snap:   awsWith(awsFixture(t, "bad.json"), func(a *collect.AWSSnapshot) { a.Cluster.StandardSupportEnds = time.Time{} }),
			status: Fail,
			want:   []string{"/EKS cluster/prod: runs Kubernetes 1.30, which is out of standard support"},
		},
		{name: "skipped when support dates cannot be read", snap: awsDeny(awsFixture(t, "good.json"), collect.ResEKSVersions), status: Skipped, skipReason: "EKS version support dates"},
	})
}
