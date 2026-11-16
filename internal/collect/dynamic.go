package collect

import (
	"context"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Resources read through the dynamic client. They belong to optional add-ons,
// so they are only listed when discovery shows the group is installed.
var (
	GVRPodMetrics      = schema.GroupVersionResource{Group: GroupMetrics, Version: "v1beta1", Resource: "pods"}
	GVRCertificates    = schema.GroupVersionResource{Group: GroupCertManager, Version: "v1", Resource: "certificates"}
	GVRVeleroBackups   = schema.GroupVersionResource{Group: GroupVelero, Version: "v1", Resource: "backups"}
	GVRVeleroSchedules = schema.GroupVersionResource{Group: GroupVelero, Version: "v1", Resource: "schedules"}
	GVRVolumeSnapshots = schema.GroupVersionResource{Group: GroupSnapshots, Version: "v1", Resource: "volumesnapshots"}
)

var dynamicKinds = map[schema.GroupKind]schema.GroupVersionResource{
	{Group: GroupMetrics, Kind: "PodMetrics"}:       GVRPodMetrics,
	{Group: GroupCertManager, Kind: "Certificate"}:  GVRCertificates,
	{Group: GroupVelero, Kind: "Backup"}:            GVRVeleroBackups,
	{Group: GroupVelero, Kind: "Schedule"}:          GVRVeleroSchedules,
	{Group: GroupSnapshots, Kind: "VolumeSnapshot"}: GVRVolumeSnapshots,
}

// DynamicResource returns the API resource for an add-on object kind the
// snapshot understands.
func DynamicResource(gk schema.GroupKind) (schema.GroupVersionResource, bool) {
	gvr, ok := dynamicKinds[gk]
	return gvr, ok
}

// DynamicListKinds maps each add-on resource to its list kind. Fake dynamic
// clients need this to serve list requests.
func DynamicListKinds() map[schema.GroupVersionResource]string {
	out := map[schema.GroupVersionResource]string{}
	for gk, gvr := range dynamicKinds {
		out[gvr] = gk.Kind + "List"
	}
	return out
}

func listDynamic(ctx context.Context, client dynamic.Interface, s *Snapshot, resource string, gvr schema.GroupVersionResource, namespaces []string) []unstructured.Unstructured {
	if client == nil {
		s.Errors[resource] = errors.New("no client available for add-on APIs")
		return nil
	}
	var out []unstructured.Unstructured
	for _, ns := range namespaces {
		l, err := client.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			s.Errors[resource] = err
			return nil
		}
		out = append(out, l.Items...)
	}
	return out
}

// addUnstructured stores an add-on object in the snapshot. It returns false
// for kinds the snapshot does not hold.
func (s *Snapshot) addUnstructured(u *unstructured.Unstructured) bool {
	gk := u.GroupVersionKind().GroupKind()
	gvr, ok := dynamicKinds[gk]
	if !ok {
		return false
	}
	ns, name := u.GetNamespace(), u.GetName()
	switch gvr {
	case GVRPodMetrics:
		usage := PodUsage{Namespace: ns, Name: name}
		containers, _, _ := unstructured.NestedSlice(u.Object, "containers")
		for _, c := range containers {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			usage.CPUMilli += quantity(m, "usage", "cpu").MilliValue()
			usage.MemoryBytes += quantity(m, "usage", "memory").Value()
		}
		s.PodMetrics = append(s.PodMetrics, usage)
	case GVRCertificates:
		s.Certificates = append(s.Certificates, Certificate{
			Namespace:   ns,
			Name:        name,
			NotAfter:    timestamp(u.Object, "status", "notAfter"),
			RenewalTime: timestamp(u.Object, "status", "renewalTime"),
		})
	case GVRVeleroBackups:
		phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
		s.VeleroBackups = append(s.VeleroBackups, VeleroBackup{
			Namespace: ns,
			Name:      name,
			Phase:     phase,
			Completed: timestamp(u.Object, "status", "completionTimestamp"),
		})
	case GVRVeleroSchedules:
		sched := VeleroSchedule{Namespace: ns, Name: name}
		sched.Paused, _, _ = unstructured.NestedBool(u.Object, "spec", "paused")
		sched.IncludedNamespaces, _, _ = unstructured.NestedStringSlice(u.Object, "spec", "template", "includedNamespaces")
		sched.ExcludedNamespaces, _, _ = unstructured.NestedStringSlice(u.Object, "spec", "template", "excludedNamespaces")
		if raw, found, _ := unstructured.NestedMap(u.Object, "spec", "template", "labelSelector"); found {
			sel := &metav1.LabelSelector{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, sel); err == nil {
				sched.LabelSelector = sel
			}
		}
		s.VeleroSchedules = append(s.VeleroSchedules, sched)
	case GVRVolumeSnapshots:
		snap := VolumeSnapshot{Namespace: ns, Name: name}
		snap.PVCName, _, _ = unstructured.NestedString(u.Object, "spec", "source", "persistentVolumeClaimName")
		snap.Ready, _, _ = unstructured.NestedBool(u.Object, "status", "readyToUse")
		snap.Created = timestamp(u.Object, "status", "creationTime")
		if snap.Created.IsZero() {
			snap.Created = u.GetCreationTimestamp().Time
		}
		s.VolumeSnapshots = append(s.VolumeSnapshots, snap)
	}
	return true
}

func timestamp(obj map[string]any, fields ...string) time.Time {
	raw, _, _ := unstructured.NestedString(obj, fields...)
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func quantity(obj map[string]any, fields ...string) *resource.Quantity {
	raw, _, _ := unstructured.NestedString(obj, fields...)
	q, err := resource.ParseQuantity(raw)
	if err != nil {
		return &resource.Quantity{}
	}
	return &q
}
