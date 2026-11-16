package collect

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // OIDC and other auth providers
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Connection is a connected cluster and what the kubeconfig says about it.
type Connection struct {
	Clients     Clients
	ClusterName string
	// EKS is the cluster name and region worked out from the kubeconfig, for
	// the AWS checks. Either part may be empty.
	EKS EKSHint
}

// Connect builds clients from the user's kubeconfig. An empty contextName
// means the current context. Nothing from the kubeconfig is kept other than
// the cluster's name and, for EKS, its region.
func Connect(contextName string) (Connection, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	raw, err := loader.RawConfig()
	if err != nil {
		return Connection{}, fmt.Errorf("loading kubeconfig: %w", err)
	}
	name := contextName
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return Connection{}, errors.New("no kubeconfig context found; set KUBECONFIG or pass --context")
	}
	kubeContext, ok := raw.Contexts[name]
	if !ok {
		return Connection{}, fmt.Errorf("kubeconfig context %q not found", name)
	}

	cfg, err := loader.ClientConfig()
	if err != nil {
		return Connection{}, fmt.Errorf("loading kubeconfig context %q: %w", name, err)
	}
	cfg.UserAgent = "shipready"
	cfg.Timeout = 60 * time.Second

	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return Connection{}, fmt.Errorf("creating Kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return Connection{}, fmt.Errorf("creating Kubernetes client: %w", err)
	}
	conn := Connection{Clients: Clients{Kube: kube, Dynamic: dyn}, ClusterName: kubeContext.Cluster}
	server := ""
	if entry := raw.Clusters[kubeContext.Cluster]; entry != nil {
		server = entry.Server
	}
	var exec *clientcmdapi.ExecConfig
	if user := raw.AuthInfos[kubeContext.AuthInfo]; user != nil {
		exec = user.Exec
	}
	conn.EKS = DetectEKS(kubeContext.Cluster, server, exec)
	return conn, nil
}
