package testutil

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
	"k8s.io/ingress-gce/pkg/flags"
	klog "k8s.io/klog/v2"
)

type FakeTracker interface {
	Add(obj runtime.Object) error
}

// EmulateProviderConfigLabelingWebhook is a helper function that emulates the behaviour
// of the providerconfig webhook.
// It will set the providerconfig name label on the object, on creation.
// This is needed, as in unit/integration tests, when we create objects, we expect
// the providerconfig name label to be set.
// The function takes a fake client and the name of the CRD.
func EmulateProviderConfigLabelingWebhook(tracker FakeTracker, fake *testing.Fake, crName string) {
	fake.PrependReactor("create", crName, func(action testing.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(testing.CreateAction)
		obj := createAction.GetObject()
		pc := obj.(metav1.Object)
		pc.GetLabels()[flags.F.ProviderConfigNameLabelKey] = pc.GetNamespace()

		err = tracker.Add(obj)
		if err != nil {
			klog.Errorf("Failed to add object to tracker: %v", err)
		}

		klog.Infof("EmulateProviderConfigWebhook: %s/%s", pc.GetNamespace(), pc.GetName())
		return true, obj, nil
	})
}
