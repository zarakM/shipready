package collect

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
)

// LoadManifests reads a multi-document YAML file of Kubernetes objects. It is
// used to build snapshots from fixtures, with no cluster involved.
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
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		objs = append(objs, obj)
	}
}

// SnapshotFromObjects builds a snapshot directly from objects, without
// namespace filtering. It rejects kinds the snapshot does not hold.
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
		default:
			return nil, fmt.Errorf("unsupported object type %T", obj)
		}
	}
	return s, nil
}
