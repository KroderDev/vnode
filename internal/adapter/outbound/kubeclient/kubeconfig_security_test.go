package kubeclient_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kroderdev/vnode/internal/adapter/outbound/kubeclient"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newResolver(objects ...corev1.Secret) *kubeclient.SecretKubeconfigResolver {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	objs := make([]corev1.Secret, len(objects))
	copy(objs, objects)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for i := range objs {
		builder = builder.WithObjects(&objs[i])
	}
	return kubeclient.NewSecretKubeconfigResolver(builder.Build())
}

// TestSecurity_KeyPriority verifies that when a secret has both "kubeconfig"
// and "config" keys, "kubeconfig" takes priority. A tenant with write access
// to the secret could try to inject a malicious kubeconfig via a secondary key.
func TestSecurity_KeyPriority(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sec", Namespace: "ns"},
		Data: map[string][]byte{
			"kubeconfig": []byte("correct"),
			"config":     []byte("injected"),
			"value":      []byte("also-injected"),
		},
	}
	resolver := newResolver(secret)

	data, err := resolver.Resolve(context.Background(), "ns", "sec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "correct" {
		t.Errorf("expected 'kubeconfig' key to win, got: %s", string(data))
	}
}

// TestSecurity_LargeKubeconfigPayload verifies the resolver doesn't panic
// on a very large kubeconfig payload.
func TestSecurity_LargeKubeconfigPayload(t *testing.T) {
	large := []byte(strings.Repeat("x", 10*1024*1024)) // 10MB
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "big", Namespace: "ns"},
		Data:       map[string][]byte{"kubeconfig": large},
	}
	resolver := newResolver(secret)

	data, err := resolver.Resolve(context.Background(), "ns", "big")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != len(large) {
		t.Errorf("expected %d bytes, got %d", len(large), len(data))
	}
}

// TestSecurity_BinaryKubeconfigData verifies the resolver handles non-UTF8
// binary data without crashing.
func TestSecurity_BinaryKubeconfigData(t *testing.T) {
	binary := []byte{0xFF, 0xFE, 0x00, 0x01, 0x80, 0x90}
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bin", Namespace: "ns"},
		Data:       map[string][]byte{"kubeconfig": binary},
	}
	resolver := newResolver(secret)

	data, err := resolver.Resolve(context.Background(), "ns", "bin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != len(binary) {
		t.Errorf("expected %d bytes, got %d", len(binary), len(data))
	}
}

// TestSecurity_CrossNamespaceAccess verifies that requesting a secret from a
// different namespace than where it exists fails (K8s API enforces this).
func TestSecurity_CrossNamespaceAccess(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sec", Namespace: "sensitive-ns"},
		Data:       map[string][]byte{"kubeconfig": []byte("data")},
	}
	resolver := newResolver(secret)

	_, err := resolver.Resolve(context.Background(), "other-ns", "sec")
	if err == nil {
		t.Fatal("expected error for cross-namespace secret access")
	}
}

// TestSecurity_SecretNamePathTraversal verifies behavior with path traversal
// characters in the secret name. K8s API rejects these, but we verify the
// resolver doesn't do anything unexpected.
func TestSecurity_SecretNamePathTraversal(t *testing.T) {
	resolver := newResolver()

	_, err := resolver.Resolve(context.Background(), "ns", "../other-secret")
	if err == nil {
		t.Fatal("expected error for secret with path traversal name")
	}
}
