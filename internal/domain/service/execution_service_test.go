package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

type fakeClusterClient struct {
	pods        map[string]model.PodSpec
	createCalls int
	updateCalls int
	deleteCalls int
	createdPods []model.PodSpec
	deletedKeys []string
	configMaps  map[string]string
	secrets     map[string]string
}

func newFakeClusterClient(pods ...model.PodSpec) *fakeClusterClient {
	client := &fakeClusterClient{pods: map[string]model.PodSpec{}}
	for _, pod := range pods {
		client.pods[pod.Namespace+"/"+pod.Name] = pod
	}
	return client
}

func (c *fakeClusterClient) CreatePod(_ context.Context, pod model.PodSpec) error {
	c.createCalls++
	c.createdPods = append(c.createdPods, pod)
	c.pods[pod.Namespace+"/"+pod.Name] = pod
	return nil
}

func (c *fakeClusterClient) UpdatePod(_ context.Context, pod model.PodSpec) error {
	c.updateCalls++
	c.pods[pod.Namespace+"/"+pod.Name] = pod
	return nil
}

func (c *fakeClusterClient) DeletePod(_ context.Context, namespace, name string) error {
	c.deleteCalls++
	key := namespace + "/" + name
	c.deletedKeys = append(c.deletedKeys, key)
	delete(c.pods, key)
	return nil
}

func (c *fakeClusterClient) GetPod(_ context.Context, namespace, name string) (*model.PodSpec, error) {
	pod, ok := c.pods[namespace+"/"+name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, name)
	}
	copy := pod
	return &copy, nil
}

func (c *fakeClusterClient) GetPodStatus(_ context.Context, namespace, name string) (*model.PodStatus, error) {
	pod, ok := c.pods[namespace+"/"+name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, name)
	}
	return &model.PodStatus{Phase: "Running", PodIP: "10.0.0.5", ContainerStatuses: []model.ContainerStatus{{Name: pod.Containers[0].Name, Ready: true, State: "running"}}}, nil
}

func (c *fakeClusterClient) ListPodsByLabels(_ context.Context, namespace string, labels map[string]string) ([]model.PodSpec, error) {
	result := make([]model.PodSpec, 0)
	for _, pod := range c.pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}
		matches := true
		for key, value := range labels {
			if pod.Labels[key] != value {
				matches = false
				break
			}
		}
		if matches {
			result = append(result, pod)
		}
	}
	return result, nil
}

type fakeTranslator struct {
	target model.PodSpec
	status model.PodStatus
}

func (t *fakeTranslator) Translate(_ context.Context, source model.PodSpec, _ model.VNodePool, _ string) (model.PodTranslation, error) {
	return model.PodTranslation{SourcePod: source, TargetPod: t.target}, nil
}

func (t *fakeTranslator) SyncStatus(_ context.Context, status model.PodStatus) (model.PodStatus, error) {
	if t.status.Phase != "" {
		return t.status, nil
	}
	return status, nil
}

func (c *fakeClusterClient) EnsureConfigMap(_ context.Context, namespace, name string, _ map[string]string, _ map[string][]byte, _ map[string]string) error {
	if c.configMaps == nil {
		c.configMaps = map[string]string{}
	}
	c.configMaps[namespace+"/"+name] = name
	return nil
}

func (c *fakeClusterClient) EnsureSecret(_ context.Context, namespace, name string, _ map[string][]byte, _ map[string]string) error {
	if c.secrets == nil {
		c.secrets = map[string]string{}
	}
	c.secrets[namespace+"/"+name] = name
	return nil
}

var _ ports.ClusterClient = (*fakeClusterClient)(nil)
var _ ports.PodTranslator = (*fakeTranslator)(nil)

func TestEnsureHostPod_RecreatesOnSpecDrift(t *testing.T) {
	existing := model.PodSpec{
		Name:               "host-pod",
		Namespace:          "tenant-a",
		Labels:             map[string]string{model.LabelManagedBy: model.LabelManagedByValue, model.LabelVNodePool: "pool-a"},
		ServiceAccountName: "sa-one",
		RuntimeClassName:   "kata",
		NodeSelector:       map[string]string{"role": "old"},
		Containers: []model.Container{{
			Name:  "app",
			Image: "nginx:1.25",
			Env:   []model.EnvVar{{Name: "VERSION", Value: "old"}},
		}},
	}
	desired := existing
	desired.NodeSelector = map[string]string{"role": "new"}
	desired.Containers = []model.Container{{
		Name:  "app",
		Image: "nginx:1.26",
		Env:   []model.EnvVar{{Name: "VERSION", Value: "new"}},
	}}

	host := newFakeClusterClient(existing)

	created, deleted, err := ensureHostPod(context.Background(), host, desired)
	if !errors.Is(err, errPodTerminating) {
		t.Fatalf("expected errPodTerminating, got %v", err)
	}
	if created {
		t.Fatal("expected host pod NOT to be created while terminating")
	}
	if !deleted {
		t.Fatal("expected drifted host pod to be deleted")
	}
	if host.createCalls != 0 {
		t.Fatalf("expected 0 create calls (deferred until next reconcile), got %d", host.createCalls)
	}
	if host.deleteCalls != 1 {
		t.Fatalf("expected 1 delete call, got %d", host.deleteCalls)
	}
}

func TestEnsureHostPod_SkipsTerminatingPod(t *testing.T) {
	existing := model.PodSpec{
		Name:      "host-pod",
		Namespace: "tenant-a",
		Labels:    map[string]string{model.LabelManagedBy: model.LabelManagedByValue, model.LabelVNodePool: "pool-a"},
		Deleting:  true,
		Containers: []model.Container{{
			Name:  "app",
			Image: "nginx:1.25",
		}},
	}

	host := newFakeClusterClient(existing)

	created, deleted, err := ensureHostPod(context.Background(), host, existing)
	if !errors.Is(err, errPodTerminating) {
		t.Fatalf("expected errPodTerminating, got %v", err)
	}
	if created || deleted {
		t.Fatal("expected no create or delete when pod is terminating")
	}
	if host.createCalls != 0 || host.deleteCalls != 0 {
		t.Fatalf("expected no API calls, got %d creates, %d deletes", host.createCalls, host.deleteCalls)
	}
}

func TestPodFromCore_PreservesExecutionFields(t *testing.T) {
	corePod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-app",
			Namespace: "tenant-a",
			Labels:    map[string]string{"app": "demo"},
		},
		Spec: corev1.PodSpec{
			NodeName:           "pool-a-1",
			ServiceAccountName: "tenant-sa",
			Containers: []corev1.Container{{
				Name:    "app",
				Image:   "nginx:1.26",
				Command: []string{"sleep"},
				Args:    []string{"3600"},
				Env:     []corev1.EnvVar{{Name: "MODE", Value: "test"}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "config",
					MountPath: "/etc/config",
					ReadOnly:  true,
				}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resourceMustParse("250m"),
						corev1.ResourceMemory: resourceMustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resourceMustParse("500m"),
						corev1.ResourceMemory: resourceMustParse("256Mi"),
					},
				},
			}},
			Volumes: []corev1.Volume{{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
					},
				},
			}},
		},
	}

	got := podFromCore(corePod)

	if got.ServiceAccountName != "tenant-sa" {
		t.Fatalf("expected service account to be preserved, got %s", got.ServiceAccountName)
	}
	if got.NodeName != "pool-a-1" {
		t.Fatalf("expected node name to be preserved, got %s", got.NodeName)
	}
	if got.Containers[0].Command[0] != "sleep" {
		t.Fatalf("expected command to be preserved, got %v", got.Containers[0].Command)
	}
	if got.Containers[0].Args[0] != "3600" {
		t.Fatalf("expected args to be preserved, got %v", got.Containers[0].Args)
	}
	if got.Containers[0].Env[0].Name != "MODE" || got.Containers[0].Env[0].Value != "test" {
		t.Fatalf("expected env to be preserved, got %+v", got.Containers[0].Env)
	}
	if got.Containers[0].VolumeMounts[0].MountPath != "/etc/config" {
		t.Fatalf("expected volume mounts to be preserved, got %+v", got.Containers[0].VolumeMounts)
	}
	if got.Containers[0].Resources.Requests.CPU != "250m" || got.Containers[0].Resources.Limits.Memory != "256Mi" {
		t.Fatalf("expected resources to be preserved, got %+v", got.Containers[0].Resources)
	}
	if len(got.Volumes) != 1 || got.Volumes[0].Type != model.VolumeTypeConfigMap || got.Volumes[0].Source != "app-config" {
		t.Fatalf("expected configmap volume to be preserved, got %+v", got.Volumes)
	}
}

func resourceMustParse(value string) resource.Quantity {
	return resource.MustParse(value)
}

func TestSyncPodResources_AvoidsCollidingTargetNames(t *testing.T) {
	host := newFakeClusterClient()
	clientset := kubefake.NewClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "a-b"},
			Data:       map[string]string{"value": "one"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "b-c", Namespace: "a"},
			Data:       map[string]string{"value": "two"},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "a-b"},
			Data:       map[string][]byte{"token": []byte("one")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "b-c", Namespace: "a"},
			Data:       map[string][]byte{"token": []byte("two")},
		},
	)
	pool := model.VNodePool{Name: "pool", Namespace: "host-ns"}

	translationA := model.TranslatePod(model.PodSpec{
		Name:      "pod-a",
		Namespace: "a-b",
		Volumes: []model.Volume{
			{Name: "cfg", Type: model.VolumeTypeConfigMap, Source: "c"},
			{Name: "sec", Type: model.VolumeTypeSecret, Source: "c"},
		},
	}, model.TranslateOpts{VNodeName: "vn", PoolName: pool.Name, TargetNamespace: pool.Namespace, RuntimeClass: "kata"})
	translationB := model.TranslatePod(model.PodSpec{
		Name:      "pod-b",
		Namespace: "a",
		Volumes: []model.Volume{
			{Name: "cfg", Type: model.VolumeTypeConfigMap, Source: "b-c"},
			{Name: "sec", Type: model.VolumeTypeSecret, Source: "b-c"},
		},
	}, model.TranslateOpts{VNodeName: "vn", PoolName: pool.Name, TargetNamespace: pool.Namespace, RuntimeClass: "kata"})

	if err := syncPodResources(context.Background(), clientset, host, translationA, pool); err != nil {
		t.Fatalf("syncing translation A: %v", err)
	}
	if err := syncPodResources(context.Background(), clientset, host, translationB, pool); err != nil {
		t.Fatalf("syncing translation B: %v", err)
	}

	refsA := translationA.ResourceRefs()
	refsB := translationB.ResourceRefs()
	if refsA[0].TargetName == refsB[0].TargetName {
		t.Fatalf("expected configmap targets to differ, got %q", refsA[0].TargetName)
	}
	if refsA[1].TargetName == refsB[1].TargetName {
		t.Fatalf("expected secret targets to differ, got %q", refsA[1].TargetName)
	}
	if len(host.configMaps) != 2 {
		t.Fatalf("expected 2 synced configmaps, got %d", len(host.configMaps))
	}
	if len(host.secrets) != 2 {
		t.Fatalf("expected 2 synced secrets, got %d", len(host.secrets))
	}
	if _, ok := host.configMaps["host-ns/"+refsA[0].TargetName]; !ok {
		t.Fatalf("expected synced configmap %q", refsA[0].TargetName)
	}
	if _, ok := host.configMaps["host-ns/"+refsB[0].TargetName]; !ok {
		t.Fatalf("expected synced configmap %q", refsB[0].TargetName)
	}
	if _, ok := host.secrets["host-ns/"+refsA[1].TargetName]; !ok {
		t.Fatalf("expected synced secret %q", refsA[1].TargetName)
	}
	if _, ok := host.secrets["host-ns/"+refsB[1].TargetName]; !ok {
		t.Fatalf("expected synced secret %q", refsB[1].TargetName)
	}
}
