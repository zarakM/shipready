package checks

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zarakM/shipready/internal/collect"
)

// Workload kinds the checks understand.
const (
	KindDeployment  = "Deployment"
	KindStatefulSet = "StatefulSet"
	KindDaemonSet   = "DaemonSet"
	KindCronJob     = "CronJob"
	KindPod         = "Pod" // bare pods only: no owner and not finished
)

// allKinds is every workload kind.
var allKinds = []string{KindDeployment, KindStatefulSet, KindDaemonSet, KindCronJob, KindPod}

var kindResource = map[string]string{
	KindDeployment:  collect.ResDeployments,
	KindStatefulSet: collect.ResStatefulSets,
	KindDaemonSet:   collect.ResDaemonSets,
	KindCronJob:     collect.ResCronJobs,
	KindPod:         collect.ResPods,
}

// workload is the common view of anything that carries a pod spec.
type workload struct {
	Namespace string
	Kind      string
	Name      string
	Replicas  int32                 // Deployments and StatefulSets only
	Selector  *metav1.LabelSelector // how the controller finds its pods; nil for CronJobs and Pods
	PodLabels map[string]string
	Spec      corev1.PodSpec
}

func (w workload) key() string {
	return w.Namespace + "/" + w.Kind + "/" + w.Name
}

// unavailable returns a skip reason if any of the kinds could not be collected.
func unavailable(s *collect.Snapshot, kinds ...string) string {
	resources := make([]string, 0, len(kinds))
	for _, k := range kinds {
		resources = append(resources, kindResource[k])
	}
	return s.Unavailable(resources...)
}

func replicas(n *int32) int32 {
	if n == nil {
		return 1 // the Kubernetes default
	}
	return *n
}

// workloads returns the in-scope workloads of the given kinds. System
// namespaces are left out on a cluster-wide scan.
func workloads(s *collect.Snapshot, kinds ...string) []workload {
	var out []workload
	for _, kind := range kinds {
		switch kind {
		case KindDeployment:
			for i := range s.Deployments {
				d := &s.Deployments[i]
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
					Replicas: replicas(d.Spec.Replicas), Selector: d.Spec.Selector,
					PodLabels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec,
				})
			}
		case KindStatefulSet:
			for i := range s.StatefulSets {
				d := &s.StatefulSets[i]
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
					Replicas: replicas(d.Spec.Replicas), Selector: d.Spec.Selector,
					PodLabels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec,
				})
			}
		case KindDaemonSet:
			for i := range s.DaemonSets {
				d := &s.DaemonSets[i]
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
					Selector:  d.Spec.Selector,
					PodLabels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec,
				})
			}
		case KindCronJob:
			for i := range s.CronJobs {
				d := &s.CronJobs[i]
				tmpl := d.Spec.JobTemplate.Spec.Template
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
					PodLabels: tmpl.Labels, Spec: tmpl.Spec,
				})
			}
		case KindPod:
			for i := range s.Pods {
				p := &s.Pods[i]
				if len(p.OwnerReferences) > 0 {
					continue // reported through its controller
				}
				if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
					continue
				}
				out = append(out, workload{
					Namespace: p.Namespace, Kind: kind, Name: p.Name,
					PodLabels: p.Labels, Spec: p.Spec,
				})
			}
		}
	}
	inScope := out[:0]
	for _, w := range out {
		if s.InScope(w.Namespace) {
			inScope = append(inScope, w)
		}
	}
	return inScope
}

// containerCheck runs problem against every regular container of the given
// workload kinds. problem returns "" when the container is fine. Init and
// ephemeral containers are not inspected.
func containerCheck(s *collect.Snapshot, kinds []string, problem func(corev1.Container) string) Result {
	return checkContainers(s, kinds, false, func(_ workload, c corev1.Container) string {
		return problem(c)
	})
}

// checkContainers is the general form: it can include init containers and
// gives problem access to the pod-level settings.
func checkContainers(s *collect.Snapshot, kinds []string, includeInit bool, problem func(workload, corev1.Container) string) Result {
	if reason := unavailable(s, kinds...); reason != "" {
		return skipped(reason)
	}
	var found []Resource
	for _, w := range workloads(s, kinds...) {
		containers := w.Spec.Containers
		if includeInit {
			containers = append(append([]corev1.Container(nil), w.Spec.InitContainers...), containers...)
		}
		for _, c := range containers {
			if detail := problem(w, c); detail != "" {
				found = append(found, Resource{
					Namespace: w.Namespace, Kind: w.Kind, Name: w.Name,
					Container: c.Name, Detail: detail,
				})
			}
		}
	}
	return fromResources(found)
}

// workloadCheck runs problem once per workload.
func workloadCheck(s *collect.Snapshot, kinds []string, problem func(workload) string) Result {
	if reason := unavailable(s, kinds...); reason != "" {
		return skipped(reason)
	}
	var found []Resource
	for _, w := range workloads(s, kinds...) {
		if detail := problem(w); detail != "" {
			found = append(found, Resource{Namespace: w.Namespace, Kind: w.Kind, Name: w.Name, Detail: detail})
		}
	}
	return fromResources(found)
}

// clusterFinding is the affected resource for a problem with the cluster as a
// whole rather than with one object in it.
func clusterFinding(s *collect.Snapshot, detail string) []Resource {
	name := s.ClusterName
	if name == "" {
		name = "cluster"
	}
	return []Resource{{Kind: "Cluster", Name: name, Detail: detail}}
}

// needsWholeCluster is the skip reason for checks that cannot judge a scan
// limited with --namespace.
const needsWholeCluster = "this check needs to see the whole cluster; run without --namespace"
