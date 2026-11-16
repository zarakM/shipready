package collect

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// EKSHint is what the kubeconfig reveals about which EKS cluster a context
// points at. Either field may be empty.
type EKSHint struct {
	Cluster string
	Region  string
}

var (
	// arn:aws:eks:us-east-1:123456789012:cluster/prod, as written by
	// `aws eks update-kubeconfig`. Covers the aws-cn and aws-us-gov partitions.
	eksARN = regexp.MustCompile(`^arn:aws[a-z-]*:eks:([a-z0-9-]+):\d+:cluster/(.+)$`)
	// prod.us-east-1.eksctl.io, as written by eksctl.
	eksctlName = regexp.MustCompile(`^(.+)\.([a-z]{2}(?:-[a-z]+)+-\d)\.eksctl\.io$`)
	// https://ABCDEF.gr7.us-east-1.eks.amazonaws.com
	eksEndpoint = regexp.MustCompile(`\.([a-z]{2}(?:-[a-z]+)+-\d)\.eks\.amazonaws\.com(\.cn)?$`)
)

// DetectEKS works out the EKS cluster name and region from one kubeconfig
// context: the cluster entry's name, the API server address and the arguments
// of the token command. It reads no credentials.
func DetectEKS(clusterEntryName, server string, exec *clientcmdapi.ExecConfig) EKSHint {
	var h EKSHint
	if m := eksARN.FindStringSubmatch(clusterEntryName); m != nil {
		h.Region, h.Cluster = m[1], m[2]
	} else if m := eksctlName.FindStringSubmatch(clusterEntryName); m != nil {
		h.Cluster, h.Region = m[1], m[2]
	}

	// `aws eks get-token --cluster-name prod --region us-east-1`, or
	// `aws-iam-authenticator token -i prod`.
	if exec != nil {
		args := exec.Args
		for i, arg := range args {
			name, value, joined := strings.Cut(arg, "=")
			if !joined {
				if i+1 >= len(args) {
					continue
				}
				value = args[i+1]
			}
			switch name {
			case "--cluster-name", "--cluster-id", "-i":
				if h.Cluster == "" {
					h.Cluster = value
				}
			case "--region":
				if h.Region == "" {
					h.Region = value
				}
			}
		}
		for _, env := range exec.Env {
			if h.Region == "" && (env.Name == "AWS_REGION" || env.Name == "AWS_DEFAULT_REGION") {
				h.Region = env.Value
			}
		}
	}

	if h.Region == "" {
		if u, err := url.Parse(server); err == nil {
			if m := eksEndpoint.FindStringSubmatch(u.Hostname()); m != nil {
				h.Region = m[1]
			}
		}
	}
	return h
}

// ConnectAWS builds read-only AWS clients using the default credential chain
// (environment, shared config and SSO, instance role). An empty region falls
// back to the one the AWS configuration names. It fails when there are no
// usable credentials or no region, so the caller can skip the AWS checks with
// a clear reason.
func ConnectAWS(ctx context.Context, region, clusterName string) (AWSClients, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return AWSClients{}, fmt.Errorf("loading AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		return AWSClients{}, errors.New("could not tell which AWS region the cluster is in; pass --aws-region")
	}
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		return AWSClients{}, errors.New("no usable AWS credentials were found; sign in (for example with `aws sso login`) or set AWS_PROFILE")
	}
	return AWSClients{
		Region:      cfg.Region,
		ClusterName: clusterName,
		EKS:         eks.NewFromConfig(cfg),
		EC2:         ec2.NewFromConfig(cfg),
		ELB:         elb.NewFromConfig(cfg),
		RDS:         rds.NewFromConfig(cfg),
	}, nil
}
