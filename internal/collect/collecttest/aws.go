package collecttest

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/smithy-go"

	"github.com/zarakM/shipready/internal/collect"
)

// FakeAWS is an in-memory AWS account. It implements every AWS client
// interface the collector uses and records each call it receives.
type FakeAWS struct {
	Cluster    *ekstypes.Cluster
	Versions   []ekstypes.ClusterVersionInformation
	NodeGroups []ekstypes.Nodegroup
	Subnets    []ec2types.Subnet
	// Volumes holds every volume in the region; DescribeVolumes applies the
	// status filter the caller sends.
	Volumes       []ec2types.Volume
	LoadBalancers []elbtypes.LoadBalancer
	TargetGroups  map[string][]elbtypes.TargetGroup             // by load balancer ARN
	TargetHealth  map[string][]elbtypes.TargetHealthDescription // by target group ARN
	DBInstances   []rdstypes.DBInstance
	DBClusters    []rdstypes.DBCluster

	// Fail makes the named operation return the error.
	Fail map[string]error
	// PageSize, when set, makes DescribeVolumes return that many per page.
	PageSize int

	mu    sync.Mutex
	calls []string
}

// Clients returns the fake as the collector's AWS client set.
func (f *FakeAWS) Clients(region, cluster string) collect.AWSClients {
	return collect.AWSClients{Region: region, ClusterName: cluster, EKS: f, EC2: f, ELB: f, RDS: f}
}

// Calls returns the operations called so far, in order.
func (f *FakeAWS) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// AccessDenied is the error AWS returns when credentials lack a permission.
func AccessDenied(operation string) error {
	return &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "not authorized to perform: " + operation}
}

func (f *FakeAWS) record(op string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op)
	return f.Fail[op]
}

func (f *FakeAWS) DescribeCluster(_ context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if err := f.record("DescribeCluster"); err != nil {
		return nil, err
	}
	if f.Cluster == nil || aws.ToString(f.Cluster.Name) != aws.ToString(in.Name) {
		return nil, &smithy.GenericAPIError{Code: "ResourceNotFoundException", Message: "No cluster found for name: " + aws.ToString(in.Name)}
	}
	return &eks.DescribeClusterOutput{Cluster: f.Cluster}, nil
}

func (f *FakeAWS) DescribeClusterVersions(_ context.Context, in *eks.DescribeClusterVersionsInput, _ ...func(*eks.Options)) (*eks.DescribeClusterVersionsOutput, error) {
	if err := f.record("DescribeClusterVersions"); err != nil {
		return nil, err
	}
	out := &eks.DescribeClusterVersionsOutput{}
	for _, v := range f.Versions {
		for _, want := range in.ClusterVersions {
			if aws.ToString(v.ClusterVersion) == want {
				out.ClusterVersions = append(out.ClusterVersions, v)
			}
		}
	}
	return out, nil
}

func (f *FakeAWS) ListNodegroups(_ context.Context, _ *eks.ListNodegroupsInput, _ ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
	if err := f.record("ListNodegroups"); err != nil {
		return nil, err
	}
	out := &eks.ListNodegroupsOutput{}
	for _, g := range f.NodeGroups {
		out.Nodegroups = append(out.Nodegroups, aws.ToString(g.NodegroupName))
	}
	return out, nil
}

func (f *FakeAWS) DescribeNodegroup(_ context.Context, in *eks.DescribeNodegroupInput, _ ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
	if err := f.record("DescribeNodegroup"); err != nil {
		return nil, err
	}
	for i := range f.NodeGroups {
		if aws.ToString(f.NodeGroups[i].NodegroupName) == aws.ToString(in.NodegroupName) {
			return &eks.DescribeNodegroupOutput{Nodegroup: &f.NodeGroups[i]}, nil
		}
	}
	return nil, &smithy.GenericAPIError{Code: "ResourceNotFoundException", Message: "node group not found"}
}

func (f *FakeAWS) DescribeSubnets(_ context.Context, in *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if err := f.record("DescribeSubnets"); err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range in.SubnetIds {
		wanted[id] = true
	}
	out := &ec2.DescribeSubnetsOutput{}
	for _, sn := range f.Subnets {
		if wanted[aws.ToString(sn.SubnetId)] {
			out.Subnets = append(out.Subnets, sn)
		}
	}
	return out, nil
}

func (f *FakeAWS) DescribeVolumes(_ context.Context, in *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	if err := f.record("DescribeVolumes"); err != nil {
		return nil, err
	}
	statuses := map[string]bool{}
	for _, filter := range in.Filters {
		if aws.ToString(filter.Name) == "status" {
			for _, v := range filter.Values {
				statuses[v] = true
			}
		}
	}
	var matching []ec2types.Volume
	for _, v := range f.Volumes {
		if len(statuses) == 0 || statuses[string(v.State)] {
			matching = append(matching, v)
		}
	}
	start, _ := strconv.Atoi(aws.ToString(in.NextToken))
	end := len(matching)
	out := &ec2.DescribeVolumesOutput{}
	if f.PageSize > 0 && start+f.PageSize < end {
		end = start + f.PageSize
		out.NextToken = aws.String(strconv.Itoa(end))
	}
	out.Volumes = matching[start:end]
	return out, nil
}

func (f *FakeAWS) DescribeLoadBalancers(_ context.Context, _ *elb.DescribeLoadBalancersInput, _ ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	if err := f.record("DescribeLoadBalancers"); err != nil {
		return nil, err
	}
	return &elb.DescribeLoadBalancersOutput{LoadBalancers: f.LoadBalancers}, nil
}

func (f *FakeAWS) DescribeTargetGroups(_ context.Context, in *elb.DescribeTargetGroupsInput, _ ...func(*elb.Options)) (*elb.DescribeTargetGroupsOutput, error) {
	if err := f.record("DescribeTargetGroups"); err != nil {
		return nil, err
	}
	return &elb.DescribeTargetGroupsOutput{TargetGroups: f.TargetGroups[aws.ToString(in.LoadBalancerArn)]}, nil
}

func (f *FakeAWS) DescribeTargetHealth(_ context.Context, in *elb.DescribeTargetHealthInput, _ ...func(*elb.Options)) (*elb.DescribeTargetHealthOutput, error) {
	if err := f.record("DescribeTargetHealth"); err != nil {
		return nil, err
	}
	return &elb.DescribeTargetHealthOutput{TargetHealthDescriptions: f.TargetHealth[aws.ToString(in.TargetGroupArn)]}, nil
}

func (f *FakeAWS) DescribeDBInstances(_ context.Context, _ *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	if err := f.record("DescribeDBInstances"); err != nil {
		return nil, err
	}
	return &rds.DescribeDBInstancesOutput{DBInstances: f.DBInstances}, nil
}

func (f *FakeAWS) DescribeDBClusters(_ context.Context, _ *rds.DescribeDBClustersInput, _ ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error) {
	if err := f.record("DescribeDBClusters"); err != nil {
		return nil, err
	}
	return &rds.DescribeDBClustersOutput{DBClusters: f.DBClusters}, nil
}

// The cluster's VPC and another one in the same account.
const (
	ClusterVPC = "vpc-0c1u5t3r"
	OtherVPC   = "vpc-0077h3r0"
)

// ProblemAccount returns an account with a problem for every AWS check, around
// an EKS cluster named "prod". It also holds resources in another VPC and
// resources that are fine, none of which should be reported.
func ProblemAccount() *FakeAWS {
	health := func(states ...elbtypes.TargetHealthStateEnum) []elbtypes.TargetHealthDescription {
		var out []elbtypes.TargetHealthDescription
		for _, s := range states {
			out = append(out, elbtypes.TargetHealthDescription{TargetHealth: &elbtypes.TargetHealth{State: s}})
		}
		return out
	}
	lb := func(name, vpc string, t elbtypes.LoadBalancerTypeEnum) elbtypes.LoadBalancer {
		return elbtypes.LoadBalancer{LoadBalancerName: aws.String(name), LoadBalancerArn: aws.String("arn:lb/" + name), VpcId: aws.String(vpc), Type: t}
	}
	tg := func(name string) elbtypes.TargetGroup {
		return elbtypes.TargetGroup{TargetGroupArn: aws.String("arn:tg/" + name)}
	}
	db := func(id, vpc string, retention int32) rdstypes.DBInstance {
		return rdstypes.DBInstance{
			DBInstanceIdentifier: aws.String(id), Engine: aws.String("postgres"),
			BackupRetentionPeriod: aws.Int32(retention),
			DBSubnetGroup:         &rdstypes.DBSubnetGroup{VpcId: aws.String(vpc)},
		}
	}
	volume := func(id string, size int32, state ec2types.VolumeState) ec2types.Volume {
		return ec2types.Volume{VolumeId: aws.String(id), Size: aws.Int32(size), VolumeType: ec2types.VolumeTypeGp3, State: state}
	}

	replica := db("orders-replica", ClusterVPC, 0)
	replica.ReadReplicaSourceDBInstanceIdentifier = aws.String("orders")
	member := db("analytics-1", ClusterVPC, 1)
	member.DBClusterIdentifier = aws.String("analytics")
	member.Engine = aws.String("aurora-postgresql")
	named := volume("vol-0aaa111", 200, ec2types.VolumeStateAvailable)
	named.Tags = []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("old-postgres-data")}}

	return &FakeAWS{
		Cluster: &ekstypes.Cluster{
			Name:    aws.String("prod"),
			Version: aws.String("1.30"),
			ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
				VpcId: aws.String(ClusterVPC), EndpointPublicAccess: true, PublicAccessCidrs: []string{"0.0.0.0/0"},
			},
			Logging: &ekstypes.Logging{ClusterLogging: []ekstypes.LogSetup{
				{Enabled: aws.Bool(true), Types: []ekstypes.LogType{ekstypes.LogTypeApi}},
				{Enabled: aws.Bool(false), Types: []ekstypes.LogType{ekstypes.LogTypeAudit, ekstypes.LogTypeAuthenticator}},
			}},
		},
		Versions: []ekstypes.ClusterVersionInformation{
			{ClusterVersion: aws.String("1.30"), VersionStatus: ekstypes.VersionStatusExtendedSupport, EndOfStandardSupportDate: aws.Time(time.Date(2025, 7, 23, 0, 0, 0, 0, time.UTC))},
			{ClusterVersion: aws.String("1.34"), VersionStatus: ekstypes.VersionStatusStandardSupport, EndOfStandardSupportDate: aws.Time(time.Date(2027, 10, 30, 0, 0, 0, 0, time.UTC))},
		},
		NodeGroups: []ekstypes.Nodegroup{
			{NodegroupName: aws.String("general"), Subnets: []string{"subnet-a1", "subnet-a2"}},
			{NodegroupName: aws.String("spot"), Subnets: []string{"subnet-a1", "subnet-b1"}},
		},
		Subnets: []ec2types.Subnet{
			{SubnetId: aws.String("subnet-a1"), AvailabilityZone: aws.String("us-east-1a")},
			{SubnetId: aws.String("subnet-a2"), AvailabilityZone: aws.String("us-east-1a")},
			{SubnetId: aws.String("subnet-b1"), AvailabilityZone: aws.String("us-east-1b")},
		},
		Volumes: []ec2types.Volume{
			named,
			volume("vol-0bbb222", 20, ec2types.VolumeStateAvailable),
			volume("vol-0ccc333", 100, ec2types.VolumeStateInUse),
			volume("vol-0ddd444", 50, ec2types.VolumeStateAvailable),
		},
		LoadBalancers: []elbtypes.LoadBalancer{
			lb("web", ClusterVPC, elbtypes.LoadBalancerTypeEnumApplication),
			lb("old-demo", ClusterVPC, elbtypes.LoadBalancerTypeEnumApplication),
			lb("admin", ClusterVPC, elbtypes.LoadBalancerTypeEnumNetwork),
			lb("elsewhere", OtherVPC, elbtypes.LoadBalancerTypeEnumApplication),
			lb("inspection", ClusterVPC, elbtypes.LoadBalancerTypeEnumGateway),
		},
		TargetGroups: map[string][]elbtypes.TargetGroup{
			"arn:lb/web":   {tg("web-a"), tg("web-b")},
			"arn:lb/admin": {tg("admin")},
		},
		TargetHealth: map[string][]elbtypes.TargetHealthDescription{
			"arn:tg/web-a": health(elbtypes.TargetHealthStateEnumHealthy, elbtypes.TargetHealthStateEnumHealthy),
			"arn:tg/web-b": health(elbtypes.TargetHealthStateEnumUnhealthy),
			"arn:tg/admin": health(elbtypes.TargetHealthStateEnumUnhealthy, elbtypes.TargetHealthStateEnumDraining),
		},
		DBInstances: []rdstypes.DBInstance{
			db("orders", ClusterVPC, 0),
			db("users", ClusterVPC, 14),
			db("other-system", OtherVPC, 0),
			replica,
			member,
		},
		DBClusters: []rdstypes.DBCluster{
			{DBClusterIdentifier: aws.String("analytics"), Engine: aws.String("aurora-postgresql"), BackupRetentionPeriod: aws.Int32(1)},
			{DBClusterIdentifier: aws.String("other-cluster"), Engine: aws.String("aurora-mysql"), BackupRetentionPeriod: aws.Int32(1)},
		},
	}
}
