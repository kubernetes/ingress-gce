package testutil

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
	"k8s.io/ingress-gce/pkg/flags"
	klog "k8s.io/klog/v2"
)

// EmulateProviderConfigLabelingWebhook is a helper function that emulates the behaviour
// of the providerconfig webhook.
// It will set the providerconfig name label on the object, on creation.
// This is needed, as in unit/integration tests, when we create objects, we expect
// the providerconfig name label to be set.
//
// Important: this function sets ProviderConfig name label to the namespace of the object.
// However, in the real world, multiple namespaces can have the same providerconfig name.
//
// The function takes a fake client and the name of the CRD.
func EmulateProviderConfigLabelingWebhook(fake *testing.Fake, crName string) {
	fake.PrependReactor("create", crName, func(action testing.Action) (handled bool, ret runtime.Object, err error) {
		createAction, ok := action.(testing.CreateAction)
		if !ok {
			return false, nil, nil
		}

		obj := createAction.GetObject()
		pc, ok := obj.(metav1.Object)
		if !ok {
			klog.Errorf("EmulateProviderConfigWebhook: object does not implement metav1.Object: %T", obj)
			return false, nil, nil
		}

		labels := pc.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[flags.F.ProviderConfigNameLabelKey] = pc.GetNamespace()
		pc.SetLabels(labels)

		klog.Infof("EmulateProviderConfigWebhook: %s/%s", pc.GetNamespace(), pc.GetName())

		// Returning false means the ReactionChain will continue and the object will be
		// stored by the standard fake reactors after we mutated it. If we returned true,
		// the reaction chain would stop here.
		return false, nil, nil
	})
}
