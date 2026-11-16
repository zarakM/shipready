package checks

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zarakM/shipready/internal/collect"
)

func TestREL001Requests(t *testing.T) {
	runCases(t, rel001, []checkCase{
		{name: "all containers have requests", snap: fixture(t, "rel/good.yaml"), status: Pass},
		{name: "missing requests", snap: fixture(t, "rel/bad.yaml"), status: Fail, want: []string{
			"shop/CronJob/cleanup[job]: no CPU request",
			"shop/DaemonSet/logs[fluent]: no CPU or memory request",
			"shop/Deployment/web[web]: no CPU or memory request",
			"shop/Pod/oneoff[main]: no CPU or memory request",
		}},
		{name: "empty cluster passes", snap: snapshot(t), status: Pass},
		{name: "skipped when pods cannot be listed", snap: forbidden(t, collect.ResPods), status: Skipped, skipReason: "pods"},
	})
}

func TestREL002MemoryLimits(t *testing.T) {
	runCases(t, rel002, []checkCase{
		{name: "all containers have memory limits", snap: fixture(t, "rel/good.yaml"), status: Pass},
		{name: "missing memory limits", snap: fixture(t, "rel/bad.yaml"), status: Fail, want: []string{
			"shop/CronJob/cleanup[job]: no memory limit",
			"shop/DaemonSet/logs[fluent]: no memory limit",
			"shop/Deployment/web[web]: no memory limit",
			"shop/Pod/oneoff[main]: no memory limit",
			"shop/StatefulSet/cache[redis]: no memory limit",
		}},
		{name: "skipped when cronjobs cannot be listed", snap: forbidden(t, collect.ResCronJobs), status: Skipped, skipReason: "cronjobs"},
	})
}

func TestREL003ReadinessProbes(t *testing.T) {
	runCases(t, rel003, []checkCase{
		{name: "all containers have readiness probes", snap: fixture(t, "rel/good.yaml"), status: Pass},
		{name: "missing readiness probes", snap: fixture(t, "rel/bad.yaml"), status: Fail, want: []string{
			"shop/Deployment/web[web]: no readiness probe",
		}},
		{name: "skipped when deployments cannot be listed", snap: forbidden(t, collect.ResDeployments), status: Skipped, skipReason: "deployments"},
		{name: "not skipped by a resource it does not use", snap: forbidden(t, collect.ResPods), status: Pass},
	})
}

func TestREL004LivenessProbes(t *testing.T) {
	runCases(t, rel004, []checkCase{
		{name: "all containers have liveness probes", snap: fixture(t, "rel/good.yaml"), status: Pass},
		{name: "missing liveness probes, cronjobs exempt", snap: fixture(t, "rel/bad.yaml"), status: Fail, want: []string{
			"shop/DaemonSet/logs[fluent]: no liveness probe",
			"shop/Deployment/web[web]: no liveness probe",
			"shop/Pod/oneoff[main]: no liveness probe",
			"shop/StatefulSet/cache[redis]: no liveness probe",
		}},
		{name: "skipped when daemonsets cannot be listed", snap: forbidden(t, collect.ResDaemonSets), status: Skipped, skipReason: "daemonsets"},
		{name: "not skipped when cronjobs cannot be listed", snap: forbidden(t, collect.ResCronJobs), status: Pass},
	})
}

func deploy(namespace, name string, replicaCount *int32, podLabels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicaCount,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: podLabels}},
		},
	}
}

func pdb(namespace, name string, selector *metav1.LabelSelector) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       policyv1.PodDisruptionBudgetSpec{Selector: selector},
	}
}

func TestREL005PodDisruptionBudgets(t *testing.T) {
	three := int32(3)
	app := map[string]string{"app": "api"}
	matchApp := &metav1.LabelSelector{MatchLabels: app}

	runCases(t, rel005, []checkCase{
		{name: "multi-replica workloads have budgets", snap: fixture(t, "rel/good.yaml"), status: Pass},
		{name: "multi-replica workload without a matching budget", snap: fixture(t, "rel/bad.yaml"), status: Fail, want: []string{
			"shop/Deployment/web: 2 replicas, no PodDisruptionBudget",
		}},
		{
			name:   "unset replicas defaults to one and is exempt",
			snap:   snapshot(t, deploy("shop", "api", nil, app)),
			status: Pass,
		},
		{
			name:   "budget in another namespace does not count",
			snap:   snapshot(t, deploy("shop", "api", &three, app), pdb("billing", "api", matchApp)),
			status: Fail,
			want:   []string{"shop/Deployment/api: 3 replicas, no PodDisruptionBudget"},
		},
		{
			name:   "empty selector covers every pod in the namespace",
			snap:   snapshot(t, deploy("shop", "api", &three, app), pdb("shop", "all", &metav1.LabelSelector{})),
			status: Pass,
		},
		{
			name:   "budget with no selector covers nothing",
			snap:   snapshot(t, deploy("shop", "api", &three, app), pdb("shop", "none", nil)),
			status: Fail,
			want:   []string{"shop/Deployment/api: 3 replicas, no PodDisruptionBudget"},
		},
		{name: "skipped when budgets cannot be listed", snap: forbidden(t, collect.ResPDBs), status: Skipped, skipReason: "poddisruptionbudgets"},
		{name: "skipped when statefulsets cannot be listed", snap: forbidden(t, collect.ResStatefulSets), status: Skipped, skipReason: "statefulsets"},
	})
}
