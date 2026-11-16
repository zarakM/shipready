package collect

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Clients are the read-only API clients the collector uses.
type Clients struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface // for optional add-on APIs
}

// Options controls what Collect fetches.
type Options struct {
	// Namespaces limits collection to these namespaces. Empty means the whole
	// cluster.
	Namespaces []string
}

// Collect lists everything the checks need. It only ever issues get and list
// requests, and it never lists Secrets. A resource type that cannot be listed
// is recorded in Snapshot.Errors and does not stop the scan; only an
// unreachable cluster or a cancelled context returns an error.
func Collect(ctx context.Context, c Clients, opts Options) (*Snapshot, error) {
	s := NewSnapshot()
	s.ClusterWide = len(opts.Namespaces) == 0
	s.CollectedAt = time.Now().UTC()

	version, err := c.Kube.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cannot reach the cluster: %w", err)
	}
	s.KubernetesVersion = version.GitVersion

	groups, err := c.Kube.Discovery().ServerGroups()
	if err != nil {
		return nil, fmt.Errorf("cannot read the cluster's API list: %w", err)
	}
	for _, g := range groups.Groups {
		s.APIGroups[g.Name] = true
	}

	k := c.Kube
	all := metav1.ListOptions{}
	cluster := []string{metav1.NamespaceAll}
	scoped := opts.Namespaces
	if len(scoped) == 0 {
		scoped = cluster
	}

	// Cluster-scoped resources.
	s.Namespaces = list(s, ResNamespaces, cluster, func(string) ([]corev1.Namespace, error) {
		l, err := k.CoreV1().Namespaces().List(ctx, all)
		if err != nil {
			return nil, err
		}
		return keepNamespaces(l.Items, opts.Namespaces), nil
	})
	s.Nodes = list(s, ResNodes, cluster, func(string) ([]corev1.Node, error) {
		l, err := k.CoreV1().Nodes().List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.PVs = list(s, ResPVs, cluster, func(string) ([]corev1.PersistentVolume, error) {
		l, err := k.CoreV1().PersistentVolumes().List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.ClusterRoles = list(s, ResClusterRoles, cluster, func(string) ([]rbacv1.ClusterRole, error) {
		l, err := k.RbacV1().ClusterRoles().List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.ClusterRoleBindings = list(s, ResClusterRoleBindings, cluster, func(string) ([]rbacv1.ClusterRoleBinding, error) {
		l, err := k.RbacV1().ClusterRoleBindings().List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})

	// Namespaced resources.
	s.Deployments = list(s, ResDeployments, scoped, func(ns string) ([]appsv1.Deployment, error) {
		l, err := k.AppsV1().Deployments(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.StatefulSets = list(s, ResStatefulSets, scoped, func(ns string) ([]appsv1.StatefulSet, error) {
		l, err := k.AppsV1().StatefulSets(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.DaemonSets = list(s, ResDaemonSets, scoped, func(ns string) ([]appsv1.DaemonSet, error) {
		l, err := k.AppsV1().DaemonSets(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.CronJobs = list(s, ResCronJobs, scoped, func(ns string) ([]batchv1.CronJob, error) {
		l, err := k.BatchV1().CronJobs(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.Pods = list(s, ResPods, scoped, func(ns string) ([]corev1.Pod, error) {
		l, err := k.CoreV1().Pods(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.PDBs = list(s, ResPDBs, scoped, func(ns string) ([]policyv1.PodDisruptionBudget, error) {
		l, err := k.PolicyV1().PodDisruptionBudgets(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.HPAs = list(s, ResHPAs, scoped, func(ns string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
		l, err := k.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.Services = list(s, ResServices, scoped, func(ns string) ([]corev1.Service, error) {
		l, err := k.CoreV1().Services(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.ServiceAccounts = list(s, ResServiceAccounts, scoped, func(ns string) ([]corev1.ServiceAccount, error) {
		l, err := k.CoreV1().ServiceAccounts(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.Roles = list(s, ResRoles, scoped, func(ns string) ([]rbacv1.Role, error) {
		l, err := k.RbacV1().Roles(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.NetworkPolicies = list(s, ResNetworkPolicies, scoped, func(ns string) ([]networkingv1.NetworkPolicy, error) {
		l, err := k.NetworkingV1().NetworkPolicies(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.PVCs = list(s, ResPVCs, scoped, func(ns string) ([]corev1.PersistentVolumeClaim, error) {
		l, err := k.CoreV1().PersistentVolumeClaims(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})

	// Optional add-ons, listed only when installed. Velero keeps its objects
	// in its own namespace, so it is always read cluster-wide.
	addons := []struct {
		group      string
		resource   string
		gvr        schema.GroupVersionResource
		namespaces []string
	}{
		{GroupMetrics, ResPodMetrics, GVRPodMetrics, scoped},
		{GroupCertManager, ResCertificates, GVRCertificates, scoped},
		{GroupSnapshots, ResVolumeSnapshots, GVRVolumeSnapshots, scoped},
		{GroupVelero, ResVeleroBackups, GVRVeleroBackups, cluster},
		{GroupVelero, ResVeleroSchedules, GVRVeleroSchedules, cluster},
	}
	for _, a := range addons {
		if !s.HasAPI(a.group) {
			continue
		}
		items := listDynamic(ctx, c.Dynamic, s, a.resource, a.gvr, a.namespaces)
		for i := range items {
			s.addUnstructured(&items[i])
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// keepNamespaces applies the --namespace filter to the namespace list.
func keepNamespaces(items []corev1.Namespace, filter []string) []corev1.Namespace {
	if len(filter) == 0 {
		return items
	}
	wanted := map[string]bool{}
	for _, ns := range filter {
		wanted[ns] = true
	}
	var out []corev1.Namespace
	for _, ns := range items {
		if wanted[ns.Name] {
			out = append(out, ns)
		}
	}
	return out
}

// list fetches one resource type from each of the given namespaces. Listing
// named namespaces one by one keeps namespace-scoped credentials working. On
// any error the resource is recorded as unavailable and no partial data is
// kept.
func list[T any](s *Snapshot, resource string, namespaces []string, fetch func(namespace string) ([]T, error)) []T {
	var out []T
	for _, ns := range namespaces {
		items, err := fetch(ns)
		if err != nil {
			s.Errors[resource] = err
			return nil
		}
		out = append(out, items...)
	}
	return out
}
