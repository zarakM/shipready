package checks

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zarakM/shipready/internal/collect"
)

const (
	secGood = "sec/good.yaml"
	secBad  = "sec/bad.yaml"
)

func TestSEC001RootContainers(t *testing.T) {
	runCases(t, sec001, []checkCase{
		{name: "non-root workloads", snap: fixture(t, secGood), status: Pass},
		{name: "root and privileged containers, init containers included", snap: fixture(t, secBad), status: Fail, want: []string{
			"shop/Deployment/storefront[init-perms]: runs as root (runAsUser: 0)",
			"shop/Deployment/storefront[sidecar]: runs privileged",
			"shop/Deployment/storefront[storefront]: nothing stops it running as root",
		}},
		{name: "skipped when deployments cannot be listed", snap: deny(fixture(t, secBad), collect.ResDeployments), status: Skipped, skipReason: "deployments"},
	})
}

func TestRootProblemPrecedence(t *testing.T) {
	yes, no := true, false
	root, user := int64(0), int64(1000)
	cases := []struct {
		name      string
		pod       *corev1.PodSecurityContext
		container *corev1.SecurityContext
		flagged   bool
	}{
		{"nothing set", nil, nil, true},
		{"pod runAsNonRoot", &corev1.PodSecurityContext{RunAsNonRoot: &yes}, nil, false},
		{"pod non-zero user", &corev1.PodSecurityContext{RunAsUser: &user}, nil, false},
		{"container overrides pod user with root", &corev1.PodSecurityContext{RunAsUser: &user}, &corev1.SecurityContext{RunAsUser: &root}, true},
		{"container overrides pod root with a user", &corev1.PodSecurityContext{RunAsUser: &root}, &corev1.SecurityContext{RunAsUser: &user}, false},
		{"container turns runAsNonRoot off", &corev1.PodSecurityContext{RunAsNonRoot: &yes}, &corev1.SecurityContext{RunAsNonRoot: &no}, true},
		{"privileged beats non-root", &corev1.PodSecurityContext{RunAsNonRoot: &yes}, &corev1.SecurityContext{Privileged: &yes}, true},
		{"privileged false is fine", &corev1.PodSecurityContext{RunAsNonRoot: &yes}, &corev1.SecurityContext{Privileged: &no}, false},
	}
	for _, tc := range cases {
		w := workload{Spec: corev1.PodSpec{SecurityContext: tc.pod}}
		c := corev1.Container{SecurityContext: tc.container}
		if got := rootProblem(w, c) != ""; got != tc.flagged {
			t.Errorf("%s: flagged = %v, want %v", tc.name, got, tc.flagged)
		}
	}
}

func TestSEC002ClusterAdmin(t *testing.T) {
	binding := func(name string, subjects ...rbacv1.Subject) *rbacv1.ClusterRoleBinding {
		return &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"},
			Subjects:   subjects,
		}
	}
	runCases(t, sec002, []checkCase{
		{name: "only system subjects", snap: fixture(t, secGood), status: Pass},
		{name: "people, pipelines and catch-all groups", snap: fixture(t, secBad), status: Fail, want: []string{
			"/ClusterRoleBinding/ci-admin: grants cluster-admin to ServiceAccount ci/deployer",
			"/ClusterRoleBinding/everyone-admin: grants cluster-admin to Group system:authenticated",
			"/ClusterRoleBinding/ops-admin: grants cluster-admin to User alice@example.com",
		}},
		{
			name:   "anonymous users are never a system subject",
			snap:   snapshot(t, binding("anon", rbacv1.Subject{Kind: "User", Name: "system:anonymous"})),
			status: Fail,
			want:   []string{"/ClusterRoleBinding/anon: grants cluster-admin to User system:anonymous"},
		},
		{
			name:   "control plane users are exempt",
			snap:   snapshot(t, binding("kcm", rbacv1.Subject{Kind: "User", Name: "system:kube-controller-manager"})),
			status: Pass,
		},
		{name: "skipped when bindings cannot be listed", snap: deny(fixture(t, secBad), collect.ResClusterRoleBindings), status: Skipped, skipReason: "clusterrolebindings"},
	})
}

func TestSEC003WildcardRoles(t *testing.T) {
	runCases(t, sec003, []checkCase{
		{name: "specific roles", snap: fixture(t, secGood), status: Pass},
		{name: "custom wildcard roles, built-ins exempt", snap: fixture(t, secBad), status: Fail, want: []string{
			"/ClusterRole/do-anything: wildcard verbs and resources",
			"shop/Role/wild-reader: wildcard resources",
		}},
		{name: "skipped when roles cannot be listed", snap: deny(fixture(t, secBad), collect.ResRoles), status: Skipped, skipReason: "roles"},
	})
}

func TestSEC004PlainSecrets(t *testing.T) {
	bad := fixture(t, secBad)
	runCases(t, sec004, []checkCase{
		{name: "secrets come from references", snap: fixture(t, secGood), status: Pass},
		{name: "secret-looking names with literal values", snap: bad, status: Fail, want: []string{
			"shop/Deployment/storefront[storefront]: plain value set for API_TOKEN, DB_PASSWORD",
		}},
		{name: "skipped when daemonsets cannot be listed", snap: deny(fixture(t, secBad), collect.ResDaemonSets), status: Skipped, skipReason: "daemonsets"},
	})

	// The hard rule: a secret value must never reach the result.
	for _, r := range sec004.Run(bad).Resources {
		if strings.Contains(r.Detail, "not-a-real") {
			t.Errorf("finding leaks an environment variable value: %q", r.Detail)
		}
	}
}

func TestPlainSecretNames(t *testing.T) {
	cases := []struct {
		env     corev1.EnvVar
		flagged bool
	}{
		{corev1.EnvVar{Name: "DB_PASSWORD", Value: "x"}, true},
		{corev1.EnvVar{Name: "stripe_api_key", Value: "x"}, true},
		{corev1.EnvVar{Name: "AWS_SECRET_ACCESS_KEY", Value: "x"}, true},
		{corev1.EnvVar{Name: "GITHUB_TOKEN", Value: "x"}, true},
		{corev1.EnvVar{Name: "DB_PASSWORD", Value: ""}, false},
		{corev1.EnvVar{Name: "DB_PASSWORD", ValueFrom: &corev1.EnvVarSource{}}, false},
		{corev1.EnvVar{Name: "DB_PASSWORD", Value: "$(OTHER_VAR)"}, false},
		{corev1.EnvVar{Name: "DB_PASSWORD_FILE", Value: "/run/secrets/db"}, false},
		{corev1.EnvVar{Name: "TOKEN_URL", Value: "https://auth.example.com/token"}, false},
		{corev1.EnvVar{Name: "SECRET_NAME", Value: "db"}, false},
		{corev1.EnvVar{Name: "LOG_LEVEL", Value: "debug"}, false},
		{corev1.EnvVar{Name: "PWD", Value: "/app"}, false},
	}
	for _, tc := range cases {
		if got := plainSecret(tc.env); got != tc.flagged {
			t.Errorf("plainSecret(%s) = %v, want %v", tc.env.Name, got, tc.flagged)
		}
	}
}

func TestSEC005NetworkPolicies(t *testing.T) {
	runCases(t, sec005, []checkCase{
		{name: "every namespace with workloads has a policy", snap: fixture(t, secGood), status: Pass},
		{name: "namespace with workloads and no policy", snap: fixture(t, secBad), status: Fail, want: []string{
			"/Namespace/shop: runs workloads with no NetworkPolicy",
		}},
		{name: "skipped when policies cannot be listed", snap: deny(fixture(t, secBad), collect.ResNetworkPolicies), status: Skipped, skipReason: "networkpolicies"},
	})
}

func TestSEC006PublicServices(t *testing.T) {
	lb := func(name string, annotations map[string]string, ranges ...string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: name, Annotations: annotations},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, LoadBalancerSourceRanges: ranges},
		}
	}
	runCases(t, sec006, []checkCase{
		{name: "internal services, NodePort on private nodes", snap: fixture(t, secGood), status: Pass},
		{name: "public load balancer and NodePort on public nodes", snap: fixture(t, secBad), status: Fail, want: []string{
			"shop/Service/nodeport: NodePort reachable on the nodes' public IP addresses",
			"shop/Service/public-lb: internet-facing load balancer with no source IP restriction",
		}},
		{
			name:   "source range open to everyone is still public",
			snap:   snapshot(t, lb("open", nil, "0.0.0.0/0")),
			status: Fail,
			want:   []string{"shop/Service/open: internet-facing load balancer with no source IP restriction"},
		},
		{
			name: "controller load balancer marked internet-facing is public",
			snap: snapshot(t, lb("facing", map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-type":   "external",
				"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing",
			})),
			status: Fail,
			want:   []string{"shop/Service/facing: internet-facing load balancer with no source IP restriction"},
		},
		{
			name: "legacy and other-cloud internal annotations",
			snap: snapshot(t,
				lb("aws-legacy", map[string]string{"service.beta.kubernetes.io/aws-load-balancer-internal": "true"}),
				lb("azure", map[string]string{"service.beta.kubernetes.io/azure-load-balancer-internal": "true"}),
				lb("gke", map[string]string{"networking.gke.io/load-balancer-type": "Internal"}),
			),
			status: Pass,
		},
		{name: "skipped when nodes cannot be listed", snap: deny(fixture(t, secBad), collect.ResNodes), status: Skipped, skipReason: "nodes"},
	})
}

func TestSEC007HostAccess(t *testing.T) {
	runCases(t, sec007, []checkCase{
		{name: "no host access", snap: fixture(t, secGood), status: Pass},
		{name: "host network and host path", snap: fixture(t, secBad), status: Fail, want: []string{
			"shop/Deployment/storefront: uses hostNetwork, hostPath volume /var/run/docker.sock",
		}},
		{name: "skipped when cronjobs cannot be listed", snap: deny(fixture(t, secBad), collect.ResCronJobs), status: Skipped, skipReason: "cronjobs"},
	})
}

func TestSEC008DefaultServiceAccount(t *testing.T) {
	runCases(t, sec008, []checkCase{
		{name: "dedicated service accounts", snap: fixture(t, secGood), status: Pass},
		{name: "default account with token mounted", snap: fixture(t, secBad), status: Fail, want: []string{
			"shop/Deployment/storefront: uses the default ServiceAccount with its token mounted",
		}},
		{name: "skipped when service accounts cannot be listed", snap: deny(fixture(t, secBad), collect.ResServiceAccounts), status: Skipped, skipReason: "serviceaccounts"},
	})
}
