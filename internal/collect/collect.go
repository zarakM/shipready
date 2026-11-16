package collect

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options controls what Collect fetches.
type Options struct {
	// Namespaces limits collection to these namespaces. Empty means every
	// namespace except the system ones.
	Namespaces []string
}

// Collect lists everything the checks need. It only ever issues get and list
// requests. A resource type that cannot be listed is recorded in
// Snapshot.Errors and does not stop the scan; only an unreachable cluster or a
// cancelled context returns an error.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) (*Snapshot, error) {
	s := NewSnapshot()

	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cannot reach the cluster: %w", err)
	}
	s.KubernetesVersion = version.GitVersion

	all := metav1.ListOptions{}

	s.Namespaces = collectNamespaces(ctx, client, s, opts)

	s.Deployments = namespaced(s, ResDeployments, opts, func(ns string) ([]appsv1.Deployment, error) {
		l, err := client.AppsV1().Deployments(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.StatefulSets = namespaced(s, ResStatefulSets, opts, func(ns string) ([]appsv1.StatefulSet, error) {
		l, err := client.AppsV1().StatefulSets(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.DaemonSets = namespaced(s, ResDaemonSets, opts, func(ns string) ([]appsv1.DaemonSet, error) {
		l, err := client.AppsV1().DaemonSets(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.CronJobs = namespaced(s, ResCronJobs, opts, func(ns string) ([]batchv1.CronJob, error) {
		l, err := client.BatchV1().CronJobs(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.Pods = namespaced(s, ResPods, opts, func(ns string) ([]corev1.Pod, error) {
		l, err := client.CoreV1().Pods(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})
	s.PDBs = namespaced(s, ResPDBs, opts, func(ns string) ([]policyv1.PodDisruptionBudget, error) {
		l, err := client.PolicyV1().PodDisruptionBudgets(ns).List(ctx, all)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func collectNamespaces(ctx context.Context, client kubernetes.Interface, s *Snapshot, opts Options) []corev1.Namespace {
	l, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		s.Errors[ResNamespaces] = err
		return nil
	}
	wanted := map[string]bool{}
	for _, ns := range opts.Namespaces {
		wanted[ns] = true
	}
	var out []corev1.Namespace
	for _, ns := range l.Items {
		if len(wanted) > 0 && !wanted[ns.Name] {
			continue
		}
		if len(wanted) == 0 && SystemNamespaces[ns.Name] {
			continue
		}
		out = append(out, ns)
	}
	return out
}

// namespaced lists one namespaced resource type. With no namespace filter it
// makes a single cluster-wide list and drops system namespaces; with a filter
// it lists each named namespace, so namespace-scoped credentials still work.
func namespaced[T any, PT interface {
	*T
	GetNamespace() string
}](s *Snapshot, resource string, opts Options, list func(namespace string) ([]T, error)) []T {
	targets := opts.Namespaces
	if len(targets) == 0 {
		targets = []string{metav1.NamespaceAll}
	}
	var out []T
	for _, ns := range targets {
		items, err := list(ns)
		if err != nil {
			s.Errors[resource] = err
			return nil
		}
		for i := range items {
			if len(opts.Namespaces) == 0 && SystemNamespaces[PT(&items[i]).GetNamespace()] {
				continue
			}
			out = append(out, items[i])
		}
	}
	return out
}
