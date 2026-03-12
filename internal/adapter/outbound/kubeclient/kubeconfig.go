package kubeclient

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ ports.KubeconfigResolver = (*SecretKubeconfigResolver)(nil)

// SecretKubeconfigResolver resolves vcluster kubeconfigs from Kubernetes Secrets.
type SecretKubeconfigResolver struct {
	client client.Client
}

func NewSecretKubeconfigResolver(c client.Client) *SecretKubeconfigResolver {
	return &SecretKubeconfigResolver{client: c}
}

func (r *SecretKubeconfigResolver) Resolve(ctx context.Context, secretNamespace, secretName string) ([]byte, error) {
	var secret corev1.Secret
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, &secret); err != nil {
		return nil, fmt.Errorf("getting kubeconfig secret %s/%s: %w", secretNamespace, secretName, err)
	}

	// Try common kubeconfig key names
	for _, key := range []string{"kubeconfig", "config", "value"} {
		if data, ok := secret.Data[key]; ok && len(data) > 0 {
			return data, nil
		}
	}

	return nil, fmt.Errorf("kubeconfig secret %s/%s has no recognized kubeconfig key (tried: kubeconfig, config, value)", secretNamespace, secretName)
}
