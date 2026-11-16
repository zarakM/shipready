// Package collect fetches cluster state once, read-only, into a Snapshot that
// checks run against. Checks never talk to the Kubernetes API themselves.
package collect

import (
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
)

// Resource names used as keys in Snapshot.Errors.
const (
	ResNamespaces   = "namespaces"
	ResDeployments  = "deployments"
	ResStatefulSets = "statefulsets"
	ResDaemonSets   = "daemonsets"
	ResCronJobs     = "cronjobs"
	ResPods         = "pods"
	ResPDBs         = "poddisruptionbudgets"
)

// SystemNamespaces are left out of workload checks unless the user names them
// with --namespace.
var SystemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// Snapshot is everything the checks need, held in memory.
type Snapshot struct {
	ClusterName       string
	KubernetesVersion string

	Namespaces   []corev1.Namespace
	Deployments  []appsv1.Deployment
	StatefulSets []appsv1.StatefulSet
	DaemonSets   []appsv1.DaemonSet
	CronJobs     []batchv1.CronJob
	Pods         []corev1.Pod
	PDBs         []policyv1.PodDisruptionBudget

	// Errors holds the reason a resource type could not be listed, keyed by
	// the Res* constants. A resource with an entry here has no data.
	Errors map[string]error
}

// NewSnapshot returns an empty snapshot.
func NewSnapshot() *Snapshot {
	return &Snapshot{Errors: map[string]error{}}
}

// Unavailable returns a plain-language reason if any of the given resource
// types could not be collected, or "" if all of them are present.
func (s *Snapshot) Unavailable(resources ...string) string {
	var reasons []string
	for _, r := range resources {
		if err, ok := s.Errors[r]; ok {
			reasons = append(reasons, fmt.Sprintf("could not list %s: %s", r, explain(err)))
		}
	}
	sort.Strings(reasons)
	return strings.Join(reasons, "; ")
}

func explain(err error) string {
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return "the scanner's credentials do not have permission"
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		return "this API is not available on the cluster"
	default:
		return err.Error()
	}
}
