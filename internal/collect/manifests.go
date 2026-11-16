package collect

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
)

// LoadManifests reads a multi-document YAML file of Kubernetes objects. It is
// used to build snapshots from fixtures, with no cluster involved. Built-in
// kinds come back typed; add-on kinds (Velero, cert-manager and so on) come
// back as unstructured objects.
func LoadManifests(path string) ([]runtime.Object, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := yaml.NewYAMLReader(bufio.NewReader(f))
	decoder := scheme.Codecs.UniversalDeserializer()
	var objs []runtime.Object
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return objs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		obj, _, err := decoder.Decode(doc, nil, nil)
		if runtime.IsNotRegisteredError(err) {
			u := &unstructured.Unstructured{}
			if err := yaml.Unmarshal(doc, &u.Object); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			objs = append(objs, u)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		objs = append(objs, obj)
	}
}

// SplitObjects separates built-in objects from add-on objects, which live in
// different fake clients in tests.
func SplitObjects(objs []runtime.Object) (typed []runtime.Object, addons []*unstructured.Unstructured) {
	for _, obj := range objs {
		if u, ok := obj.(*unstructured.Unstructured); ok {
			addons = append(addons, u)
		} else {
			typed = append(typed, obj)
		}
	}
	return typed, addons
}

// SnapshotFromObjects builds a cluster-wide snapshot directly from objects.
// The API group of every add-on object is marked as installed. It rejects
// kinds the snapshot does not hold.
func SnapshotFromObjects(objs []runtime.Object) (*Snapshot, error) {
	s := NewSnapshot()
	for _, obj := range objs {
		switch o := obj.(type) {
		case *corev1.Namespace:
			s.Namespaces = append(s.Namespaces, *o)
		case *appsv1.Deployment:
			s.Deployments = append(s.Deployments, *o)
		case *appsv1.StatefulSet:
			s.StatefulSets = append(s.StatefulSets, *o)
		case *appsv1.DaemonSet:
			s.DaemonSets = append(s.DaemonSets, *o)
		case *batchv1.CronJob:
			s.CronJobs = append(s.CronJobs, *o)
		case *corev1.Pod:
			s.Pods = append(s.Pods, *o)
		case *policyv1.PodDisruptionBudget:
			s.PDBs = append(s.PDBs, *o)
		case *corev1.Node:
			s.Nodes = append(s.Nodes, *o)
		case *autoscalingv2.HorizontalPodAutoscaler:
			s.HPAs = append(s.HPAs, *o)
		case *corev1.Service:
			s.Services = append(s.Services, *o)
		case *corev1.ServiceAccount:
			s.ServiceAccounts = append(s.ServiceAccounts, *o)
		case *rbacv1.Role:
			s.Roles = append(s.Roles, *o)
		case *rbacv1.ClusterRole:
			s.ClusterRoles = append(s.ClusterRoles, *o)
		case *rbacv1.ClusterRoleBinding:
			s.ClusterRoleBindings = append(s.ClusterRoleBindings, *o)
		case *networkingv1.NetworkPolicy:
			s.NetworkPolicies = append(s.NetworkPolicies, *o)
		case *corev1.PersistentVolume:
			s.PVs = append(s.PVs, *o)
		case *corev1.PersistentVolumeClaim:
			s.PVCs = append(s.PVCs, *o)
		case *unstructured.Unstructured:
			if !s.addUnstructured(o) {
				return nil, fmt.Errorf("unsupported object kind %s", o.GroupVersionKind())
			}
			s.APIGroups[o.GroupVersionKind().Group] = true
		default:
			return nil, fmt.Errorf("unsupported object type %T", obj)
		}
	}
	return s, nil
}
