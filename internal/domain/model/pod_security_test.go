package model_test

import (
	"strings"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
)

// TestSecurity_SAMountPathVariants verifies that isServiceAccountMount uses
// exact-match on the canonical path. Variants with trailing slashes or
// subdirectory suffixes are NOT stripped. This documents the current behavior
// as a known gap — a tenant pod could mount a subdirectory of the SA token
// path and bypass the stripping logic.
func TestSecurity_SAMountPathVariants(t *testing.T) {
	variants := []struct {
		path      string
		expectStr bool // true = expected to be stripped
	}{
		{"/var/run/secrets/kubernetes.io/serviceaccount", true},
		{"/var/run/secrets/kubernetes.io/serviceaccount/", false},       // GAP: trailing slash
		{"/var/run/secrets/kubernetes.io/serviceaccount/token", false},  // GAP: subdirectory
		{"/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", false}, // GAP: subdirectory
		{"/var/run/secrets", false},
		{"/tmp", false},
	}

	for _, v := range variants {
		source := model.PodSpec{
			Name: "test", Namespace: "ns",
			Containers: []model.Container{
				{
					Name: "c", Image: "img",
					VolumeMounts: []model.VolumeMount{
						{Name: "sa", MountPath: v.path},
					},
				},
			},
		}
		result := model.TranslatePod(source, opts("vn", "pool", "host-ns", "kata"))
		mounts := result.TargetPod.Containers[0].VolumeMounts
		stripped := len(mounts) == 0

		if stripped != v.expectStr {
			t.Errorf("path %q: stripped=%v, want %v", v.path, stripped, v.expectStr)
		}
	}
}

// TestSecurity_AllSystemLabelsCannotBeSpoofed verifies that a source pod
// attempting to override ALL five system labels has none of them accepted.
func TestSecurity_AllSystemLabelsCannotBeSpoofed(t *testing.T) {
	systemLabels := map[string]string{
		model.LabelManagedBy:     "attacker",
		model.LabelVNodePool:     "fake-pool",
		model.LabelVNodeName:     "fake-node",
		model.LabelSourcePodName: "fake-name",
		model.LabelSourcePodNS:   "fake-ns",
	}

	source := model.PodSpec{
		Name: "victim", Namespace: "tenant-ns",
		Labels: systemLabels,
	}
	result := model.TranslatePod(source, opts("real-node", "real-pool", "host-ns", "kata"))
	target := result.TargetPod

	if target.Labels[model.LabelManagedBy] != model.LabelManagedByValue {
		t.Errorf("LabelManagedBy spoofed: got %s", target.Labels[model.LabelManagedBy])
	}
	if target.Labels[model.LabelVNodePool] != "real-pool" {
		t.Errorf("LabelVNodePool spoofed: got %s", target.Labels[model.LabelVNodePool])
	}
	if target.Labels[model.LabelVNodeName] != "real-node" {
		t.Errorf("LabelVNodeName spoofed: got %s", target.Labels[model.LabelVNodeName])
	}
	if target.Labels[model.LabelSourcePodName] != "victim" {
		t.Errorf("LabelSourcePodName spoofed: got %s", target.Labels[model.LabelSourcePodName])
	}
	if target.Labels[model.LabelSourcePodNS] != "tenant-ns" {
		t.Errorf("LabelSourcePodNS spoofed: got %s", target.Labels[model.LabelSourcePodNS])
	}
}

// TestSecurity_LabelSpecialCharacters verifies translation doesn't panic on
// labels with special characters (newlines, null bytes, long values).
func TestSecurity_LabelSpecialCharacters(t *testing.T) {
	source := model.PodSpec{
		Name: "test", Namespace: "ns",
		Labels: map[string]string{
			"key-with-newline":     "value\ninjection",
			"key-with-null":        "value\x00null",
			"key-with-long-value":  strings.Repeat("a", 10000),
			strings.Repeat("k", 1000): "long-key",
		},
	}
	result := model.TranslatePod(source, opts("vn", "pool", "host-ns", "kata"))
	target := result.TargetPod

	// System labels must remain intact
	if target.Labels[model.LabelManagedBy] != model.LabelManagedByValue {
		t.Error("system labels corrupted by special character labels")
	}
	// Special char labels should be copied through
	if target.Labels["key-with-newline"] != "value\ninjection" {
		t.Error("newline label not preserved")
	}
}

// TestSecurity_LargeNumberOfLabels verifies translation handles a large number
// of source labels without issues, and system labels remain correct.
func TestSecurity_LargeNumberOfLabels(t *testing.T) {
	labels := make(map[string]string, 500)
	for i := 0; i < 500; i++ {
		labels[strings.Repeat("label-", 1)+string(rune('a'+i%26))+strings.Repeat("x", i%50)] = "v"
	}

	source := model.PodSpec{
		Name: "test", Namespace: "ns",
		Labels: labels,
	}
	result := model.TranslatePod(source, opts("vn", "pool", "host-ns", "kata"))

	if result.TargetPod.Labels[model.LabelManagedBy] != model.LabelManagedByValue {
		t.Error("system labels corrupted by large label set")
	}
}

// TestSecurity_HostPathVolumesNotFiltered documents that HostPath volumes are
// NOT filtered during translation. This is a security gap — a tenant pod could
// mount arbitrary host filesystem paths on the underlying node.
func TestSecurity_HostPathVolumesNotFiltered(t *testing.T) {
	source := model.PodSpec{
		Name: "test", Namespace: "ns",
		Volumes: []model.Volume{
			{Name: "host-root", Type: model.VolumeTypeHostPath, Source: "/"},
			{Name: "host-etc", Type: model.VolumeTypeHostPath, Source: "/etc"},
			{Name: "host-var", Type: model.VolumeTypeHostPath, Source: "/var/log"},
		},
	}
	result := model.TranslatePod(source, opts("vn", "pool", "host-ns", "kata"))

	// GAP: All HostPath volumes pass through unfiltered
	if len(result.TargetPod.Volumes) != 3 {
		t.Errorf("expected 3 HostPath volumes (not filtered), got %d", len(result.TargetPod.Volumes))
	}
	for _, v := range result.TargetPod.Volumes {
		if v.Type != model.VolumeTypeHostPath {
			t.Errorf("unexpected volume type: %s", v.Type)
		}
	}
}

// TestSecurity_EmptyRuntimeClass verifies that an empty RuntimeClass in
// TranslateOpts produces a pod with no runtime isolation, meaning pods
// will run on the default container runtime (typically runc).
func TestSecurity_EmptyRuntimeClass(t *testing.T) {
	source := model.PodSpec{Name: "test", Namespace: "ns"}
	result := model.TranslatePod(source, opts("vn", "pool", "host-ns", ""))

	if result.TargetPod.RuntimeClassName != "" {
		t.Errorf("expected empty RuntimeClassName, got %s", result.TargetPod.RuntimeClassName)
	}
}

// TestSecurity_PodNameOverflow verifies behavior when inputs produce a
// translated pod name exceeding the Kubernetes 253-character limit.
// Currently no truncation is applied — this documents the gap.
func TestSecurity_PodNameOverflow(t *testing.T) {
	longName := strings.Repeat("a", 100)
	longNS := strings.Repeat("b", 100)
	longVNode := strings.Repeat("c", 100)

	source := model.PodSpec{Name: longName, Namespace: longNS}
	result := model.TranslatePod(source, opts(longVNode, "pool", "host-ns", "kata"))

	// Format: {vnode}-{namespace}-{name} = 100 + 1 + 100 + 1 + 100 = 302 chars
	expectedLen := len(longVNode) + 1 + len(longNS) + 1 + len(longName)
	if len(result.TargetPod.Name) != expectedLen {
		t.Errorf("expected name length %d, got %d", expectedLen, len(result.TargetPod.Name))
	}

	// GAP: Name exceeds K8s 253-char limit with no truncation
	if len(result.TargetPod.Name) <= 253 {
		t.Error("expected name to exceed 253 chars to document the gap")
	}
}

// TestSecurity_ServiceAccountNameNotPropagated verifies that the source pod's
// ServiceAccountName is never carried to the host pod.
func TestSecurity_ServiceAccountNameNotPropagated(t *testing.T) {
	source := model.PodSpec{
		Name:               "test",
		Namespace:          "ns",
		ServiceAccountName: "admin-sa",
	}
	result := model.TranslatePod(source, opts("vn", "pool", "host-ns", "kata"))

	if result.TargetPod.ServiceAccountName != "" {
		t.Errorf("ServiceAccountName should not propagate to host pod, got %s", result.TargetPod.ServiceAccountName)
	}
}
