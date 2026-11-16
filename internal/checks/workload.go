package checks

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zarakM/shipready/internal/collect"
)

// Workload kinds the reliability checks understand.
const (
	KindDeployment  = "Deployment"
	KindStatefulSet = "StatefulSet"
	KindDaemonSet   = "DaemonSet"
	KindCronJob     = "CronJob"
	KindPod         = "Pod" // bare pods only: no owner and not finished
)

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
	Replicas  int32 // Deployments and StatefulSets only
	PodLabels map[string]string
	Spec      corev1.PodSpec
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

// workloads returns the workloads of the given kinds.
func workloads(s *collect.Snapshot, kinds ...string) []workload {
	var out []workload
	for _, kind := range kinds {
		switch kind {
		case KindDeployment:
			for i := range s.Deployments {
				d := &s.Deployments[i]
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
					Replicas:  replicas(d.Spec.Replicas),
					PodLabels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec,
				})
			}
		case KindStatefulSet:
			for i := range s.StatefulSets {
				d := &s.StatefulSets[i]
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
					Replicas:  replicas(d.Spec.Replicas),
					PodLabels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec,
				})
			}
		case KindDaemonSet:
			for i := range s.DaemonSets {
				d := &s.DaemonSets[i]
				out = append(out, workload{
					Namespace: d.Namespace, Kind: kind, Name: d.Name,
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
	return out
}

// containerCheck runs problem against every regular container of the given
// workload kinds. problem returns "" when the container is fine. Init and
// ephemeral containers are deliberately not inspected.
func containerCheck(s *collect.Snapshot, kinds []string, problem func(corev1.Container) string) Result {
	if reason := unavailable(s, kinds...); reason != "" {
		return skipped(reason)
	}
	var found []Resource
	for _, w := range workloads(s, kinds...) {
		for _, c := range w.Spec.Containers {
			if detail := problem(c); detail != "" {
				found = append(found, Resource{
					Namespace: w.Namespace, Kind: w.Kind, Name: w.Name,
					Container: c.Name, Detail: detail,
				})
			}
		}
	}
	return fromResources(found)
}
