package translator

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTranslate(t *testing.T) {
	tr := New("vcluster-org1", "kata", "vnode-01")

	tests := []struct {
		name     string
		pod      *corev1.Pod
		wantName string
	}{
		{
			name: "basic pod translation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nginx",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "original-node",
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
					ServiceAccountName: "my-sa",
					Tolerations: []corev1.Toleration{
						{Key: "node.kubernetes.io/not-ready"},
					},
				},
			},
			wantName: "vnode-01-default-nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tr.Translate(tt.pod)

			if result.Name != tt.wantName {
				t.Errorf("name = %q, want %q", result.Name, tt.wantName)
			}
			if result.Namespace != "vcluster-org1" {
				t.Errorf("namespace = %q, want %q", result.Namespace, "vcluster-org1")
			}
			if result.Spec.RuntimeClassName == nil || *result.Spec.RuntimeClassName != "kata" {
				t.Error("RuntimeClassName should be kata")
			}
			if result.Spec.NodeName != "" {
				t.Errorf("NodeName = %q, want empty", result.Spec.NodeName)
			}
			if result.Spec.ServiceAccountName != "" {
				t.Error("ServiceAccountName should be cleared")
			}
			if result.Spec.Tolerations != nil {
				t.Errorf("Tolerations should be nil, got %v", result.Spec.Tolerations)
			}
			if result.Spec.AutomountServiceAccountToken == nil || *result.Spec.AutomountServiceAccountToken != false {
				t.Error("AutomountServiceAccountToken should be false")
			}

			// Verify labels
			if result.Labels[LabelManagedBy] != ManagedByValue {
				t.Errorf("missing managed-by label")
			}
			if result.Labels[LabelVNodeName] != "vnode-01" {
				t.Errorf("missing vnode name label")
			}
			if result.Labels[LabelVClusterPodName] != "nginx" {
				t.Errorf("missing vcluster pod name label")
			}
			if result.Labels[LabelVClusterPodNamespace] != "default" {
				t.Errorf("missing vcluster pod namespace label")
			}

			// Verify resources are preserved
			cpu := result.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU]
			if cpu.String() != "500m" {
				t.Errorf("cpu limit = %q, want 500m", cpu.String())
			}
		})
	}
}

func TestTranslateFiltersServiceAccountMounts(t *testing.T) {
	tr := New("ns", "kata", "vnode-01")

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "app:v1",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: "/data"},
						{Name: "kube-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "kube-token", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{}}},
			},
		},
	}

	result := tr.Translate(pod)

	if len(result.Spec.Containers[0].VolumeMounts) != 1 {
		t.Errorf("expected 1 volume mount, got %d", len(result.Spec.Containers[0].VolumeMounts))
	}
	if len(result.Spec.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(result.Spec.Volumes))
	}
}

func TestMatchLabels(t *testing.T) {
	tr := New("ns", "kata", "vnode-01")
	labels := tr.MatchLabels()

	if labels[LabelManagedBy] != ManagedByValue {
		t.Error("missing managed-by label")
	}
	if labels[LabelVNodeName] != "vnode-01" {
		t.Error("missing vnode name label")
	}
}

func TestHostPodName(t *testing.T) {
	tr := New("ns", "kata", "vnode-01")
	name := tr.HostPodName("default", "nginx")
	if name != "vnode-01-default-nginx" {
		t.Errorf("got %q, want %q", name, "vnode-01-default-nginx")
	}
}
