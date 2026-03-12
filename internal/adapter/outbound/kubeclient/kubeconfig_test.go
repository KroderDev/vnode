package kubeclient_test

import (
	"context"
	"testing"

	"github.com/kroderdev/vnode/internal/adapter/outbound/kubeclient"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSecretKubeconfigResolver_Resolve_KubeconfigKey(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vc-kubeconfig", Namespace: "default"},
		Data:       map[string][]byte{"kubeconfig": []byte("apiVersion: v1\nclusters: []")},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	resolver := kubeclient.NewSecretKubeconfigResolver(fakeClient)

	data, err := resolver.Resolve(context.Background(), "default", "vc-kubeconfig")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "apiVersion: v1\nclusters: []" {
		t.Errorf("unexpected kubeconfig data: %s", string(data))
	}
}

func TestSecretKubeconfigResolver_Resolve_ConfigKey(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vc-secret", Namespace: "ns"},
		Data:       map[string][]byte{"config": []byte("kube-data")},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	resolver := kubeclient.NewSecretKubeconfigResolver(fakeClient)

	data, err := resolver.Resolve(context.Background(), "ns", "vc-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "kube-data" {
		t.Errorf("unexpected data: %s", string(data))
	}
}

func TestSecretKubeconfigResolver_Resolve_ValueKey(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vc-secret", Namespace: "ns"},
		Data:       map[string][]byte{"value": []byte("value-data")},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	resolver := kubeclient.NewSecretKubeconfigResolver(fakeClient)

	data, err := resolver.Resolve(context.Background(), "ns", "vc-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "value-data" {
		t.Errorf("unexpected data: %s", string(data))
	}
}

func TestSecretKubeconfigResolver_Resolve_SecretNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	resolver := kubeclient.NewSecretKubeconfigResolver(fakeClient)

	_, err := resolver.Resolve(context.Background(), "ns", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestSecretKubeconfigResolver_Resolve_NoRecognizedKey(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-secret", Namespace: "ns"},
		Data:       map[string][]byte{"other-key": []byte("data")},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	resolver := kubeclient.NewSecretKubeconfigResolver(fakeClient)

	_, err := resolver.Resolve(context.Background(), "ns", "bad-secret")
	if err == nil {
		t.Fatal("expected error for unrecognized key")
	}
}

func TestSecretKubeconfigResolver_Resolve_EmptyData(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-secret", Namespace: "ns"},
		Data:       map[string][]byte{"kubeconfig": {}},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	resolver := kubeclient.NewSecretKubeconfigResolver(fakeClient)

	_, err := resolver.Resolve(context.Background(), "ns", "empty-secret")
	if err == nil {
		t.Fatal("expected error for empty kubeconfig data")
	}
}
