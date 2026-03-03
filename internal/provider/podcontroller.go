package provider

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	vknode "github.com/virtual-kubelet/virtual-kubelet/node"
)

// NewPodControllerConfig builds a PodControllerConfig wired to the vcluster API.
func NewPodControllerConfig(ctx context.Context, prov *Provider, vclusterClient kubernetes.Interface, nodeName string) (*vknode.PodControllerConfig, error) {
	// Create informer factory scoped to pods assigned to this node
	factory := informers.NewSharedInformerFactoryWithOptions(
		vclusterClient,
		1*time.Minute,
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
		}),
	)

	podInformer := factory.Core().V1().Pods()
	configMapInformer := factory.Core().V1().ConfigMaps()
	secretInformer := factory.Core().V1().Secrets()
	serviceInformer := factory.Core().V1().Services()

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{
		Interface: vclusterClient.CoreV1().Events("default"),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{
		Component: fmt.Sprintf("vnode-%s", nodeName),
	})

	return &vknode.PodControllerConfig{
		PodClient:         vclusterClient.CoreV1(),
		PodInformer:       podInformer,
		EventRecorder:     recorder,
		Provider:          prov,
		ConfigMapInformer: configMapInformer,
		SecretInformer:    secretInformer,
		ServiceInformer:   serviceInformer,
	}, nil
}
