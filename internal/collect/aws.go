package collect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/smithy-go"
)

// AWS resource names used as keys in AWSSnapshot.Errors. They appear in skip
// reasons.
const (
	ResEKSCluster    = "the EKS cluster"
	ResEKSVersions   = "EKS version support dates"
	ResEKSNodeGroups = "EKS node groups"
	ResEBSVolumes    = "EBS volumes"
	ResLoadBalancers = "load balancers"
	ResRDS           = "RDS databases"
)

// EKS version support states, as the EKS API reports them.
const (
	VersionStandardSupport = "STANDARD_SUPPORT"
	VersionExtendedSupport = "EXTENDED_SUPPORT"
	VersionUnsupported     = "UNSUPPORTED"
)

const awsNotRequested = "AWS checks were not requested; run with --aws to include them"

// The AWS client interfaces hold only the calls the collector makes. Every
// method is a Describe, List or Get, and a test enforces that, so the scanner
// cannot change anything in the account.

// EKSAPI is the read-only slice of the EKS API the collector uses.
type EKSAPI interface {
	DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	DescribeClusterVersions(context.Context, *eks.DescribeClusterVersionsInput, ...func(*eks.Options)) (*eks.DescribeClusterVersionsOutput, error)
	ListNodegroups(context.Context, *eks.ListNodegroupsInput, ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error)
	DescribeNodegroup(context.Context, *eks.DescribeNodegroupInput, ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
}

// EC2API is the read-only slice of the EC2 API the collector uses.
type EC2API interface {
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeVolumes(context.Context, *ec2.DescribeVolumesInput, ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
}

// ELBAPI is the read-only slice of the load balancing API the collector uses.
type ELBAPI interface {
	DescribeLoadBalancers(context.Context, *elb.DescribeLoadBalancersInput, ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
	DescribeTargetGroups(context.Context, *elb.DescribeTargetGroupsInput, ...func(*elb.Options)) (*elb.DescribeTargetGroupsOutput, error)
	DescribeTargetHealth(context.Context, *elb.DescribeTargetHealthInput, ...func(*elb.Options)) (*elb.DescribeTargetHealthOutput, error)
}

// RDSAPI is the read-only slice of the RDS API the collector uses.
type RDSAPI interface {
	DescribeDBInstances(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
	DescribeDBClusters(context.Context, *rds.DescribeDBClustersInput, ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error)
}

// AWSClients are the read-only AWS clients for one region, plus which EKS
// cluster the scan is about.
type AWSClients struct {
	Region      string
	ClusterName string // empty when it could not be worked out
	EKS         EKSAPI
	EC2         EC2API
	ELB         ELBAPI
	RDS         RDSAPI
}

// EKSCluster is what the checks need to know about the control plane.
type EKSCluster struct {
	Name           string
	Version        string
	VPCID          string
	EndpointPublic bool
	PublicCIDRs    []string
	EnabledLogs    []string // control plane log types that are switched on

	// Version support, from the EKS API. VersionStatus is empty when it could
	// not be read.
	VersionStatus       string
	StandardSupportEnds time.Time
}

// EKSNodeGroup is one managed node group and the zones its subnets span.
type EKSNodeGroup struct {
	Name  string
	Zones []string
}

// EBSVolume is one unattached volume.
type EBSVolume struct {
	ID      string
	Name    string // from the Name tag, often empty
	SizeGiB int32
	Type    string
}

// LoadBalancer is one ALB or NLB in the cluster's VPC.
type LoadBalancer struct {
	Name              string
	Type              string // "application" or "network"
	TargetGroups      int
	RegisteredTargets int
	HealthyTargets    int
}

// Database is one RDS instance or cluster in the cluster's VPC.
type Database struct {
	ID            string
	Engine        string
	Cluster       bool // an Aurora or Multi-AZ DB cluster, not a single instance
	RetentionDays int32
}

// AWSSnapshot is the AWS side of a scan. Nothing in it identifies the account:
// no account ID, no ARNs and nothing from the credentials.
type AWSSnapshot struct {
	Region      string
	ClusterName string

	Cluster       *EKSCluster // nil when the cluster could not be read
	NodeGroups    []EKSNodeGroup
	Volumes       []EBSVolume
	LoadBalancers []LoadBalancer
	Databases     []Database

	// Errors holds the reason a resource type could not be read, keyed by the
	// Res* constants above.
	Errors map[string]error `json:"-"`
}

// Unavailable returns a plain-language reason if any of the given resource
// types could not be collected, or "" if all of them are present.
func (a *AWSSnapshot) Unavailable(resources ...string) string {
	var reasons []string
	for _, r := range resources {
		if err, ok := a.Errors[r]; ok {
			reasons = append(reasons, fmt.Sprintf("could not read %s: %s", r, explain(err)))
		}
	}
	sort.Strings(reasons)
	return strings.Join(reasons, "; ")
}

// AWSData returns the AWS part of the snapshot, or the reason there is none:
// either --aws was not given, or AWS could not be reached at all.
func (s *Snapshot) AWSData() (*AWSSnapshot, string) {
	if s.AWS != nil {
		return s.AWS, ""
	}
	if s.AWSSkipReason != "" {
		return nil, s.AWSSkipReason
	}
	return nil, awsNotRequested
}

// awsDenied reports whether an AWS error means the credentials lack
// permission.
func awsDenied(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "UnauthorizedAccess", "AuthorizationError":
		return true
	}
	return false
}

// errNoVPC explains why VPC-scoped resources were not read.
var errNoVPC = errors.New("the cluster's VPC is not known, so there is nothing to scope the search to")

// CollectAWS reads everything the AWS checks need. It only makes Describe and
// List calls. As with the cluster collector, a resource type that cannot be
// read is recorded in Errors and the rest of the scan carries on.
//
// Load balancers and databases are limited to the cluster's VPC, so resources
// belonging to other systems in the account are not reported. Unattached EBS
// volumes are read for the whole region, since a volume has no VPC.
func CollectAWS(ctx context.Context, c AWSClients) *AWSSnapshot {
	a := &AWSSnapshot{Region: c.Region, ClusterName: c.ClusterName, Errors: map[string]error{}}

	collectEKS(ctx, c, a)
	collectVolumes(ctx, c, a)

	vpc := ""
	if a.Cluster != nil {
		vpc = a.Cluster.VPCID
	}
	if vpc == "" {
		a.Errors[ResLoadBalancers] = errNoVPC
		a.Errors[ResRDS] = errNoVPC
		return a
	}
	collectLoadBalancers(ctx, c, a, vpc)
	collectDatabases(ctx, c, a, vpc)
	return a
}

func collectEKS(ctx context.Context, c AWSClients, a *AWSSnapshot) {
	if c.ClusterName == "" {
		err := errors.New("could not tell which EKS cluster this kubeconfig context points at; pass --eks-cluster")
		a.Errors[ResEKSCluster] = err
		a.Errors[ResEKSVersions] = err
		a.Errors[ResEKSNodeGroups] = err
		return
	}
	out, err := c.EKS.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(c.ClusterName)})
	if err != nil || out.Cluster == nil {
		if err == nil {
			err = errors.New("the cluster was not found")
		}
		a.Errors[ResEKSCluster] = err
		a.Errors[ResEKSVersions] = err
		a.Errors[ResEKSNodeGroups] = err
		return
	}
	cl := out.Cluster
	cluster := &EKSCluster{Name: aws.ToString(cl.Name), Version: aws.ToString(cl.Version)}
	if vpc := cl.ResourcesVpcConfig; vpc != nil {
		cluster.VPCID = aws.ToString(vpc.VpcId)
		cluster.EndpointPublic = vpc.EndpointPublicAccess
		cluster.PublicCIDRs = vpc.PublicAccessCidrs
	}
	if cl.Logging != nil {
		for _, setup := range cl.Logging.ClusterLogging {
			if !aws.ToBool(setup.Enabled) {
				continue
			}
			for _, t := range setup.Types {
				cluster.EnabledLogs = append(cluster.EnabledLogs, string(t))
			}
		}
		sort.Strings(cluster.EnabledLogs)
	}
	a.Cluster = cluster

	// Support dates come from AWS at scan time, so no table in the binary can
	// go stale.
	versions, err := c.EKS.DescribeClusterVersions(ctx, &eks.DescribeClusterVersionsInput{ClusterVersions: []string{cluster.Version}})
	switch {
	case err != nil:
		a.Errors[ResEKSVersions] = err
	case len(versions.ClusterVersions) == 0:
		a.Errors[ResEKSVersions] = fmt.Errorf("AWS returned no support information for Kubernetes %s", cluster.Version)
	default:
		v := versions.ClusterVersions[0]
		cluster.VersionStatus = strings.ToUpper(string(v.VersionStatus))
		cluster.StandardSupportEnds = aws.ToTime(v.EndOfStandardSupportDate)
	}

	a.NodeGroups, err = nodeGroups(ctx, c)
	if err != nil {
		a.Errors[ResEKSNodeGroups] = err
	}
}

func nodeGroups(ctx context.Context, c AWSClients) ([]EKSNodeGroup, error) {
	var names []string
	pages := eks.NewListNodegroupsPaginator(c.EKS, &eks.ListNodegroupsInput{ClusterName: aws.String(c.ClusterName)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		names = append(names, page.Nodegroups...)
	}

	groupSubnets := map[string][]string{}
	var allSubnets []string
	seen := map[string]bool{}
	for _, name := range names {
		out, err := c.EKS.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
			ClusterName: aws.String(c.ClusterName), NodegroupName: aws.String(name),
		})
		if err != nil {
			return nil, err
		}
		if out.Nodegroup == nil {
			continue
		}
		groupSubnets[name] = out.Nodegroup.Subnets
		for _, id := range out.Nodegroup.Subnets {
			if !seen[id] {
				seen[id] = true
				allSubnets = append(allSubnets, id)
			}
		}
	}

	zoneOf := map[string]string{}
	if len(allSubnets) > 0 {
		pages := ec2.NewDescribeSubnetsPaginator(c.EC2, &ec2.DescribeSubnetsInput{SubnetIds: allSubnets})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, sn := range page.Subnets {
				zoneOf[aws.ToString(sn.SubnetId)] = aws.ToString(sn.AvailabilityZone)
			}
		}
	}

	groups := make([]EKSNodeGroup, 0, len(names))
	for _, name := range names {
		zones := map[string]bool{}
		for _, id := range groupSubnets[name] {
			if z := zoneOf[id]; z != "" {
				zones[z] = true
			}
		}
		g := EKSNodeGroup{Name: name}
		for z := range zones {
			g.Zones = append(g.Zones, z)
		}
		sort.Strings(g.Zones)
		groups = append(groups, g)
	}
	return groups, nil
}

func collectVolumes(ctx context.Context, c AWSClients, a *AWSSnapshot) {
	pages := ec2.NewDescribeVolumesPaginator(c.EC2, &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{{Name: aws.String("status"), Values: []string{"available"}}},
	})
	var volumes []EBSVolume
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			a.Errors[ResEBSVolumes] = err
			return
		}
		for _, v := range page.Volumes {
			vol := EBSVolume{ID: aws.ToString(v.VolumeId), SizeGiB: aws.ToInt32(v.Size), Type: string(v.VolumeType)}
			for _, tag := range v.Tags {
				if aws.ToString(tag.Key) == "Name" {
					vol.Name = aws.ToString(tag.Value)
				}
			}
			volumes = append(volumes, vol)
		}
	}
	a.Volumes = volumes
}

func collectLoadBalancers(ctx context.Context, c AWSClients, a *AWSSnapshot, vpc string) {
	var found []LoadBalancer
	pages := elb.NewDescribeLoadBalancersPaginator(c.ELB, &elb.DescribeLoadBalancersInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			a.Errors[ResLoadBalancers] = err
			return
		}
		for _, lb := range page.LoadBalancers {
			if aws.ToString(lb.VpcId) != vpc {
				continue
			}
			if lb.Type != elbtypes.LoadBalancerTypeEnumApplication && lb.Type != elbtypes.LoadBalancerTypeEnumNetwork {
				continue // gateway load balancers have no health to judge here
			}
			item := LoadBalancer{Name: aws.ToString(lb.LoadBalancerName), Type: string(lb.Type)}
			groups, err := c.ELB.DescribeTargetGroups(ctx, &elb.DescribeTargetGroupsInput{LoadBalancerArn: lb.LoadBalancerArn})
			if err != nil {
				a.Errors[ResLoadBalancers] = err
				return
			}
			item.TargetGroups = len(groups.TargetGroups)
			for _, tg := range groups.TargetGroups {
				health, err := c.ELB.DescribeTargetHealth(ctx, &elb.DescribeTargetHealthInput{TargetGroupArn: tg.TargetGroupArn})
				if err != nil {
					a.Errors[ResLoadBalancers] = err
					return
				}
				for _, t := range health.TargetHealthDescriptions {
					item.RegisteredTargets++
					if t.TargetHealth != nil && t.TargetHealth.State == elbtypes.TargetHealthStateEnumHealthy {
						item.HealthyTargets++
					}
				}
			}
			found = append(found, item)
		}
	}
	a.LoadBalancers = found
}

func collectDatabases(ctx context.Context, c AWSClients, a *AWSSnapshot, vpc string) {
	var found []Database
	// Instances that belong to a DB cluster take their backup settings from
	// the cluster, so the cluster is judged instead of each member.
	clustersInVPC := map[string]bool{}

	instances := rds.NewDescribeDBInstancesPaginator(c.RDS, &rds.DescribeDBInstancesInput{})
	for instances.HasMorePages() {
		page, err := instances.NextPage(ctx)
		if err != nil {
			a.Errors[ResRDS] = err
			return
		}
		for _, db := range page.DBInstances {
			if db.DBSubnetGroup == nil || aws.ToString(db.DBSubnetGroup.VpcId) != vpc {
				continue
			}
			if id := aws.ToString(db.DBClusterIdentifier); id != "" {
				clustersInVPC[id] = true
				continue
			}
			// A read replica is rebuilt from its source, not from backups.
			if aws.ToString(db.ReadReplicaSourceDBInstanceIdentifier) != "" || aws.ToString(db.ReadReplicaSourceDBClusterIdentifier) != "" {
				continue
			}
			found = append(found, Database{
				ID: aws.ToString(db.DBInstanceIdentifier), Engine: aws.ToString(db.Engine),
				RetentionDays: aws.ToInt32(db.BackupRetentionPeriod),
			})
		}
	}

	clusters := rds.NewDescribeDBClustersPaginator(c.RDS, &rds.DescribeDBClustersInput{})
	for clusters.HasMorePages() {
		page, err := clusters.NextPage(ctx)
		if err != nil {
			a.Errors[ResRDS] = err
			return
		}
		for _, cl := range page.DBClusters {
			id := aws.ToString(cl.DBClusterIdentifier)
			if !clustersInVPC[id] || aws.ToString(cl.ReplicationSourceIdentifier) != "" {
				continue
			}
			found = append(found, Database{
				ID: id, Engine: aws.ToString(cl.Engine), Cluster: true,
				RetentionDays: aws.ToInt32(cl.BackupRetentionPeriod),
			})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].ID < found[j].ID })
	a.Databases = found
}
