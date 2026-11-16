package collect

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // OIDC and other auth providers
	"k8s.io/client-go/tools/clientcmd"
)

// Connect builds a client from the user's kubeconfig. An empty contextName
// means the current context. It returns the client and the cluster name the
// context points at. Nothing from the kubeconfig other than that name is kept.
func Connect(contextName string) (kubernetes.Interface, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	raw, err := loader.RawConfig()
	if err != nil {
		return nil, "", fmt.Errorf("loading kubeconfig: %w", err)
	}
	name := contextName
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return nil, "", errors.New("no kubeconfig context found; set KUBECONFIG or pass --context")
	}
	kubeContext, ok := raw.Contexts[name]
	if !ok {
		return nil, "", fmt.Errorf("kubeconfig context %q not found", name)
	}

	cfg, err := loader.ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("loading kubeconfig context %q: %w", name, err)
	}
	cfg.UserAgent = "shipready"
	cfg.Timeout = 60 * time.Second

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("creating Kubernetes client: %w", err)
	}
	return client, kubeContext.Cluster, nil
}
