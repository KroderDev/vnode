package model_test

import (
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
)

func TestTranslatePod_BasicTranslation(t *testing.T) {
	source := model.PodSpec{
		Name:               "my-app",
		Namespace:          "tenant-ns",
		ServiceAccountName: "default",
		Labels: map[string]string{
			"app": "my-app",
		},
		Containers: []model.Container{
			{
				Name:  "main",
				Image: "nginx:latest",
				VolumeMounts: []model.VolumeMount{
					{Name: "sa-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
					{Name: "data", MountPath: "/data"},
				},
			},
		},
		Volumes: []model.Volume{
			{Name: "sa-token", Type: model.VolumeTypeProjected},
			{Name: "data", Type: model.VolumeTypeEmptyDir},
		},
	}

	result := model.TranslatePod(source, "vnode-1", "pool-medium", "host-ns", "kata")
	target := result.TargetPod

	if target.Name != "vnode-1-tenant-ns-my-app" {
		t.Errorf("expected name vnode-1-tenant-ns-my-app, got %s", target.Name)
	}
	if target.Namespace != "host-ns" {
		t.Errorf("expected namespace host-ns, got %s", target.Namespace)
	}
	if target.RuntimeClassName != "kata" {
		t.Errorf("expected runtimeClass kata, got %s", target.RuntimeClassName)
	}
	if target.Labels[model.LabelManagedBy] != model.LabelManagedByValue {
		t.Error("missing managed-by label")
	}
	if target.Labels["app"] != "my-app" {
		t.Error("missing original app label")
	}
	if target.Labels[model.LabelVNodeName] != "vnode-1" {
		t.Error("missing vnode-name label")
	}
	if target.Labels[model.LabelVNodePool] != "pool-medium" {
		t.Error("missing pool label")
	}
	if target.Labels[model.LabelSourcePodName] != "my-app" {
		t.Error("missing source pod name label")
	}
	if target.Labels[model.LabelSourcePodNS] != "tenant-ns" {
		t.Error("missing source pod namespace label")
	}

	// Projected volumes stripped
	for _, v := range target.Volumes {
		if v.Type == model.VolumeTypeProjected {
			t.Error("projected volume should have been stripped")
		}
	}
	if len(target.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(target.Volumes))
	}

	// SA token mount stripped
	if len(target.Containers[0].VolumeMounts) != 1 {
		t.Errorf("expected 1 volume mount, got %d", len(target.Containers[0].VolumeMounts))
	}
	if target.Containers[0].VolumeMounts[0].Name != "data" {
		t.Errorf("expected data volume mount, got %s", target.Containers[0].VolumeMounts[0].Name)
	}

	// ServiceAccountName not carried
	if target.ServiceAccountName != "" {
		t.Errorf("expected empty service account, got %s", target.ServiceAccountName)
	}
}

func TestTranslatePod_PreservesContainerResources(t *testing.T) {
	source := model.PodSpec{
		Name:      "app",
		Namespace: "ns",
		Containers: []model.Container{
			{
				Name:  "main",
				Image: "nginx",
				Resources: model.ContainerResources{
					Requests: model.ResourceList{CPU: "100m", Memory: "128Mi"},
					Limits:   model.ResourceList{CPU: "500m", Memory: "512Mi"},
				},
			},
		},
	}

	result := model.TranslatePod(source, "vn-1", "pool", "host-ns", "kata")
	res := result.TargetPod.Containers[0].Resources
	if res.Requests.CPU != "100m" {
		t.Errorf("expected CPU request 100m, got %s", res.Requests.CPU)
	}
	if res.Limits.Memory != "512Mi" {
		t.Errorf("expected memory limit 512Mi, got %s", res.Limits.Memory)
	}
}

func TestTranslatePod_EmptyContainers(t *testing.T) {
	source := model.PodSpec{
		Name:      "empty",
		Namespace: "ns",
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if len(result.TargetPod.Containers) != 0 {
		t.Errorf("expected 0 containers, got %d", len(result.TargetPod.Containers))
	}
}

func TestTranslatePod_MultipleContainers(t *testing.T) {
	source := model.PodSpec{
		Name:      "multi",
		Namespace: "ns",
		Containers: []model.Container{
			{Name: "app", Image: "app:v1"},
			{Name: "sidecar", Image: "proxy:v1"},
			{Name: "init", Image: "init:v1"},
		},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if len(result.TargetPod.Containers) != 3 {
		t.Errorf("expected 3 containers, got %d", len(result.TargetPod.Containers))
	}
	for i, name := range []string{"app", "sidecar", "init"} {
		if result.TargetPod.Containers[i].Name != name {
			t.Errorf("container %d: expected name %s, got %s", i, name, result.TargetPod.Containers[i].Name)
		}
	}
}

func TestTranslatePod_MultipleVolumes_MixedTypes(t *testing.T) {
	source := model.PodSpec{
		Name:      "vol-test",
		Namespace: "ns",
		Volumes: []model.Volume{
			{Name: "proj1", Type: model.VolumeTypeProjected},
			{Name: "cm", Type: model.VolumeTypeConfigMap},
			{Name: "proj2", Type: model.VolumeTypeProjected},
			{Name: "secret", Type: model.VolumeTypeSecret},
			{Name: "empty", Type: model.VolumeTypeEmptyDir},
		},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	// Only non-projected volumes should remain
	if len(result.TargetPod.Volumes) != 3 {
		t.Errorf("expected 3 non-projected volumes, got %d", len(result.TargetPod.Volumes))
	}
	for _, v := range result.TargetPod.Volumes {
		if v.Type == model.VolumeTypeProjected {
			t.Errorf("projected volume %s should have been stripped", v.Name)
		}
	}
}

func TestTranslatePod_NoSourceLabels(t *testing.T) {
	source := model.PodSpec{
		Name:      "no-labels",
		Namespace: "ns",
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	// Should still have vnode system labels (managed-by, pool, node-name, source-pod-name, source-pod-ns)
	if len(result.TargetPod.Labels) != 5 {
		t.Errorf("expected 5 vnode labels, got %d", len(result.TargetPod.Labels))
	}
	if result.TargetPod.Labels[model.LabelManagedBy] != model.LabelManagedByValue {
		t.Error("missing managed-by label")
	}
}

func TestTranslatePod_VNodeLabelsNotOverriddenBySource(t *testing.T) {
	source := model.PodSpec{
		Name:      "override",
		Namespace: "ns",
		Labels: map[string]string{
			model.LabelManagedBy: "someone-else",
			model.LabelVNodeName: "fake-node",
			"custom":             "value",
		},
	}
	result := model.TranslatePod(source, "real-node", "pool", "host-ns", "kata")
	// System labels must NOT be overridden by source
	if result.TargetPod.Labels[model.LabelManagedBy] != model.LabelManagedByValue {
		t.Errorf("managed-by label should not be overridden, got %s", result.TargetPod.Labels[model.LabelManagedBy])
	}
	if result.TargetPod.Labels[model.LabelVNodeName] != "real-node" {
		t.Errorf("vnode-name label should not be overridden, got %s", result.TargetPod.Labels[model.LabelVNodeName])
	}
	// Custom labels should still be copied
	if result.TargetPod.Labels["custom"] != "value" {
		t.Error("custom label should be preserved")
	}
}

func TestTranslatePod_ContainerWithOnlySAMount(t *testing.T) {
	source := model.PodSpec{
		Name:      "sa-only",
		Namespace: "ns",
		Containers: []model.Container{
			{
				Name:  "main",
				Image: "app:v1",
				VolumeMounts: []model.VolumeMount{
					{Name: "sa-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount"},
				},
			},
		},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if len(result.TargetPod.Containers[0].VolumeMounts) != 0 {
		t.Errorf("expected 0 volume mounts after stripping SA, got %d", len(result.TargetPod.Containers[0].VolumeMounts))
	}
}

func TestTranslatePod_ContainerWithNoMounts(t *testing.T) {
	source := model.PodSpec{
		Name:      "no-mounts",
		Namespace: "ns",
		Containers: []model.Container{
			{Name: "main", Image: "app:v1"},
		},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if len(result.TargetPod.Containers[0].VolumeMounts) != 0 {
		t.Errorf("expected 0 volume mounts, got %d", len(result.TargetPod.Containers[0].VolumeMounts))
	}
}

func TestTranslatePod_PreservesEnvAndArgs(t *testing.T) {
	source := model.PodSpec{
		Name:      "env-test",
		Namespace: "ns",
		Containers: []model.Container{
			{
				Name:    "main",
				Image:   "app:v1",
				Command: []string{"/bin/sh", "-c"},
				Args:    []string{"echo hello"},
				Env: []model.EnvVar{
					{Name: "FOO", Value: "bar"},
					{Name: "BAZ", Value: "qux"},
				},
			},
		},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	c := result.TargetPod.Containers[0]

	if len(c.Command) != 2 || c.Command[0] != "/bin/sh" {
		t.Errorf("command not preserved: %v", c.Command)
	}
	if len(c.Args) != 1 || c.Args[0] != "echo hello" {
		t.Errorf("args not preserved: %v", c.Args)
	}
	if len(c.Env) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(c.Env))
	}
	if c.Env[0].Name != "FOO" || c.Env[0].Value != "bar" {
		t.Errorf("env[0] not preserved: %+v", c.Env[0])
	}
}

func TestTranslatePod_SourcePodPreservedInResult(t *testing.T) {
	source := model.PodSpec{
		Name:      "src",
		Namespace: "ns",
		Labels:    map[string]string{"app": "test"},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if result.SourcePod.Name != "src" {
		t.Errorf("source pod not preserved in translation result")
	}
	if result.SourcePod.Namespace != "ns" {
		t.Errorf("source pod namespace not preserved")
	}
}

func TestTranslatePod_EmptyVolumes(t *testing.T) {
	source := model.PodSpec{
		Name:      "no-vols",
		Namespace: "ns",
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if len(result.TargetPod.Volumes) != 0 {
		t.Errorf("expected 0 volumes, got %d", len(result.TargetPod.Volumes))
	}
}

func TestTranslatePod_AllVolumeTypesPreservedExceptProjected(t *testing.T) {
	types := []model.VolumeType{
		model.VolumeTypeConfigMap,
		model.VolumeTypeSecret,
		model.VolumeTypeEmptyDir,
		model.VolumeTypePVC,
		model.VolumeTypeHostPath,
		model.VolumeTypeOther,
	}
	var vols []model.Volume
	for i, vt := range types {
		vols = append(vols, model.Volume{Name: string(rune('a' + i)), Type: vt})
	}

	source := model.PodSpec{Name: "all-types", Namespace: "ns", Volumes: vols}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")
	if len(result.TargetPod.Volumes) != len(types) {
		t.Errorf("expected %d volumes (all non-projected types), got %d", len(types), len(result.TargetPod.Volumes))
	}
}

func TestTranslatePod_MultipleContainersWithMixedMounts(t *testing.T) {
	source := model.PodSpec{
		Name:      "mixed",
		Namespace: "ns",
		Containers: []model.Container{
			{
				Name:  "c1",
				Image: "img1",
				VolumeMounts: []model.VolumeMount{
					{Name: "sa", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount"},
					{Name: "data", MountPath: "/data"},
				},
			},
			{
				Name:  "c2",
				Image: "img2",
				VolumeMounts: []model.VolumeMount{
					{Name: "config", MountPath: "/config"},
				},
			},
			{
				Name:  "c3",
				Image: "img3",
				VolumeMounts: []model.VolumeMount{
					{Name: "sa", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount"},
				},
			},
		},
	}
	result := model.TranslatePod(source, "vn", "pool", "host-ns", "kata")

	// c1: 1 mount (data), SA stripped
	if len(result.TargetPod.Containers[0].VolumeMounts) != 1 {
		t.Errorf("c1: expected 1 mount, got %d", len(result.TargetPod.Containers[0].VolumeMounts))
	}
	// c2: 1 mount (config), no SA to strip
	if len(result.TargetPod.Containers[1].VolumeMounts) != 1 {
		t.Errorf("c2: expected 1 mount, got %d", len(result.TargetPod.Containers[1].VolumeMounts))
	}
	// c3: 0 mounts, only SA stripped
	if len(result.TargetPod.Containers[2].VolumeMounts) != 0 {
		t.Errorf("c3: expected 0 mounts, got %d", len(result.TargetPod.Containers[2].VolumeMounts))
	}
}
