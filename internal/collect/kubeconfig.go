package collect

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // OIDC and other auth providers
	"k8s.io/client-go/tools/clientcmd"
)

// Connect builds clients from the user's kubeconfig. An empty contextName
// means the current context. It returns the clients and the cluster name the
// context points at. Nothing from the kubeconfig other than that name is kept.
func Connect(contextName string) (Clients, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	raw, err := loader.RawConfig()
	if err != nil {
		return Clients{}, "", fmt.Errorf("loading kubeconfig: %w", err)
	}
	name := contextName
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return Clients{}, "", errors.New("no kubeconfig context found; set KUBECONFIG or pass --context")
	}
	kubeContext, ok := raw.Contexts[name]
	if !ok {
		return Clients{}, "", fmt.Errorf("kubeconfig context %q not found", name)
	}

	cfg, err := loader.ClientConfig()
	if err != nil {
		return Clients{}, "", fmt.Errorf("loading kubeconfig context %q: %w", name, err)
	}
	cfg.UserAgent = "shipready"
	cfg.Timeout = 60 * time.Second

	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return Clients{}, "", fmt.Errorf("creating Kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return Clients{}, "", fmt.Errorf("creating Kubernetes client: %w", err)
	}
	return Clients{Kube: kube, Dynamic: dyn}, kubeContext.Cluster, nil
}
