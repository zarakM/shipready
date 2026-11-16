// Package collect fetches cluster state once, read-only, into a Snapshot that
// checks run against. Checks never talk to the Kubernetes API themselves.
package collect

import (
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Resource names used as keys in Snapshot.Errors. They appear in skip reasons,
// so they are written to be readable. Secrets are deliberately absent: the
// collector never lists them.
const (
	ResNamespaces          = "namespaces"
	ResDeployments         = "deployments"
	ResStatefulSets        = "statefulsets"
	ResDaemonSets          = "daemonsets"
	ResCronJobs            = "cronjobs"
	ResPods                = "pods"
	ResPDBs                = "poddisruptionbudgets"
	ResNodes               = "nodes"
	ResHPAs                = "horizontalpodautoscalers"
	ResServices            = "services"
	ResServiceAccounts     = "serviceaccounts"
	ResRoles               = "roles"
	ResClusterRoles        = "clusterroles"
	ResClusterRoleBindings = "clusterrolebindings"
	ResNetworkPolicies     = "networkpolicies"
	ResPVs                 = "persistentvolumes"
	ResPVCs                = "persistentvolumeclaims"
	ResPodMetrics          = "pod metrics"
	ResCertificates        = "cert-manager certificates"
	ResVeleroBackups       = "Velero backups"
	ResVeleroSchedules     = "Velero schedules"
	ResVolumeSnapshots     = "volume snapshots"
)

// API groups whose presence the checks care about.
const (
	GroupMetrics     = "metrics.k8s.io"
	GroupCertManager = "cert-manager.io"
	GroupVelero      = "velero.io"
	GroupSnapshots   = "snapshot.storage.k8s.io"
	GroupKarpenter   = "karpenter.sh"
)

// SystemNamespaces are left out of workload checks unless the user names them
// with --namespace.
var SystemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// PodUsage is one pod's current usage from metrics-server.
type PodUsage struct {
	Namespace   string
	Name        string
	CPUMilli    int64
	MemoryBytes int64
}

// Certificate is a cert-manager Certificate's expiry, read from its status.
// The TLS Secret behind it is never opened.
type Certificate struct {
	Namespace   string
	Name        string
	NotAfter    time.Time // zero until the certificate is issued
	RenewalTime time.Time // zero if cert-manager has not scheduled a renewal
}

// VeleroBackup is the outcome of one Velero backup.
type VeleroBackup struct {
	Namespace string
	Name      string
	Phase     string
	Completed time.Time
}

// VeleroSchedule is what one Velero schedule covers.
type VeleroSchedule struct {
	Namespace          string
	Name               string
	Paused             bool
	IncludedNamespaces []string
	ExcludedNamespaces []string
	LabelSelector      *metav1.LabelSelector
}

// VolumeSnapshot is one CSI snapshot of a PersistentVolumeClaim.
type VolumeSnapshot struct {
	Namespace string
	Name      string
	PVCName   string
	Ready     bool
	Created   time.Time
}

// Snapshot is everything the checks need, held in memory.
type Snapshot struct {
	ClusterName       string
	KubernetesVersion string
	CollectedAt       time.Time

	// ClusterWide is false when the scan was limited with --namespace. Checks
	// that need to see the whole cluster skip themselves in that case.
	ClusterWide bool

	// APIGroups is the set of API groups the cluster serves.
	APIGroups map[string]bool

	Namespaces          []corev1.Namespace
	Deployments         []appsv1.Deployment
	StatefulSets        []appsv1.StatefulSet
	DaemonSets          []appsv1.DaemonSet
	CronJobs            []batchv1.CronJob
	Pods                []corev1.Pod
	PDBs                []policyv1.PodDisruptionBudget
	Nodes               []corev1.Node
	HPAs                []autoscalingv2.HorizontalPodAutoscaler
	Services            []corev1.Service
	ServiceAccounts     []corev1.ServiceAccount
	Roles               []rbacv1.Role
	ClusterRoles        []rbacv1.ClusterRole
	ClusterRoleBindings []rbacv1.ClusterRoleBinding
	NetworkPolicies     []networkingv1.NetworkPolicy
	PVs                 []corev1.PersistentVolume
	PVCs                []corev1.PersistentVolumeClaim

	// Collected only when the matching API group is installed.
	PodMetrics      []PodUsage
	Certificates    []Certificate
	VeleroBackups   []VeleroBackup
	VeleroSchedules []VeleroSchedule
	VolumeSnapshots []VolumeSnapshot

	// Errors holds the reason a resource type could not be listed, keyed by
	// the Res* constants. A resource with an entry here has no data.
	Errors map[string]error
}

// NewSnapshot returns an empty cluster-wide snapshot.
func NewSnapshot() *Snapshot {
	return &Snapshot{
		ClusterWide: true,
		APIGroups:   map[string]bool{},
		Errors:      map[string]error{},
	}
}

// InScope reports whether workload checks should look at a namespace. On a
// cluster-wide scan the snapshot holds system namespaces too, so that checks
// can detect tools installed there, but their workloads are not judged. With
// --namespace, whatever the user named is in scope.
func (s *Snapshot) InScope(namespace string) bool {
	return !s.ClusterWide || !SystemNamespaces[namespace]
}

// HasAPI reports whether the cluster serves an API group.
func (s *Snapshot) HasAPI(group string) bool {
	return s.APIGroups[group]
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
