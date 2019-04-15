/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// IngressStability denotes the stabilization status of all Ingresses in a sandbox.
type IngressStability string

var (
	// Stable indicates an Ingress is stable (i.e consistently serving 200's)
	Stable IngressStability = "Stable"
	// Unstable indicates an Ingress is unstable (i.e serving 404/502's).
	Unstable IngressStability = "Unstable"
)

const (
	configMapName  = "status-cm"
	cmPollInterval = 30 * time.Second
	flushInterval  = 30 * time.Second
	// ExitKey is the key used to indicate to the status manager
	// whether to gracefully finish the e2e test execution.
	// Value associated with it is a timestamp string.
	exitKey = "exit"
	// masterUpgradingKey is the key used to indicate to the status manager that
	// the k8s master is in the process of upgrading.
	// Value associated with it is a timestamp string.
	masterUpgradingKey = "master-upgrading"
	// masterUpgradedKey is the key used to indicate to the status manager that
	// the k8s master has successfully finished upgrading.
	// Value associated with it is a timestamp string.
	masterUpgradedKey = "master-upgraded"
)

// StatusManager manages the status of sandboxed Ingresses via a ConfigMap.
// It interacts with the an external framework test portion as follows:
// 1. StatusManager initializes and creates the ConfigMap status-cm. It listens
// on updates via informers.
// 2. e2e test calls StatusManager.putStatus with the Ingress name as key,
// and Unstable as the status
// 3. e2e test watches for when Ingress stabilizes, then uses StatusManager to
// update the Ingress's status to Stable
// 4. The external framework test reads from ConfigMap status-cm. When it detects that all
// Ingresses are stable (i.e., no value in the map is Unstable), it starts
// the MasterUpgrade.
// 5. When the k8s master finishes upgrading, the framework test writes the
// timestamp to the master-upgraded key in the ConfigMap
// 6. The external framework test writes the exit key in the ConfigMap to indicate that the e2e
// test can exit.
// 7. The StatusManager loop reads the exit key, then starts shutdown().
type StatusManager struct {
	cm              *v1.ConfigMap
	f               *Framework
	informerCh      chan struct{}
	informerRunning bool
}

func NewStatusManager(f *Framework) *StatusManager {
	return &StatusManager{
		cm: &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: configMapName,
			},
		},
		f: f,
	}
}

func (sm *StatusManager) init() error {
	var err error

	// Optimistically delete a leftover config map
	if err := sm.f.Clientset.CoreV1().ConfigMaps("default").Delete(sm.cm.Name, &metav1.DeleteOptions{}); !errors.IsNotFound(err) {
		klog.V(3).Infof("Error deleting config map %s: %v", sm.cm.Name, err)
	}

	sm.cm, err = sm.f.Clientset.CoreV1().ConfigMaps("default").Create(sm.cm)
	if err != nil {
		return fmt.Errorf("error creating ConfigMap: %v", err)
	}

	go func() {
		for _ = range time.NewTicker(flushInterval).C {
			sm.flush()
		}
	}()

	sm.startInformer()
	return nil
}

func (sm *StatusManager) startInformer() {
	newIndexer := func() cache.Indexers {
		return cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	}

	sm.informerCh = make(chan struct{})
	cmInformer := informerv1.NewConfigMapInformer(sm.f.Clientset, "default", cmPollInterval, newIndexer())
	cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, cur interface{}) {
			curCm := cur.(*v1.ConfigMap)
			upgradeTs := curCm.Data[masterUpgradingKey]
			// We need to pick up the master-upgrading flag here, because otherwise
			// if the k8s master is in the process of upgrading we can't access
			// the ConfigMap.
			if len(upgradeTs) > 0 {
				klog.V(2).Infof("Master upgrade began at %v", upgradeTs)
				sm.setMasterUpgrading(upgradeTs)
			}

			if len(curCm.Data[exitKey]) > 0 {
				klog.V(2).Infof("ConfigMap was updated with exit switch at %s", curCm.Data[exitKey])
				close(sm.informerCh)
				sm.f.shutdown(0)
			}
		},
	})

	klog.V(2).Info("Started ConfigMap informer")
	sm.informerRunning = true
	go cmInformer.Run(sm.informerCh)
}

func (sm *StatusManager) stopInformer() {
	klog.V(2).Info("Stopped ConfigMap informer")
	sm.informerRunning = false
	close(sm.informerCh)
}

func (sm *StatusManager) shutdown() {
	klog.V(2).Infof("Shutting down status manager.")
	klog.V(3).Infof("ConfigMap: %+v", sm.cm.Data)
	if err := sm.f.Clientset.CoreV1().ConfigMaps("default").Delete(configMapName, &metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Error deleting ConfigMap: %v", err)
	}
}

func (sm *StatusManager) putStatus(key string, status IngressStability) {
	sm.f.lock.Lock()
	defer sm.f.lock.Unlock()
	if sm.cm.Data == nil {
		sm.cm.Data = make(map[string]string)
	}
	sm.cm.Data[key] = string(status)
}

func (sm *StatusManager) setMasterUpgrading(ts string) {
	sm.f.lock.Lock()
	defer sm.f.lock.Unlock()
	if sm.cm.Data == nil {
		sm.cm.Data = make(map[string]string)
	}

	sm.cm.Data[masterUpgradingKey] = ts
}

func (sm *StatusManager) masterUpgrading() bool {
	sm.f.lock.Lock()
	defer sm.f.lock.Unlock()

	return len(sm.cm.Data[masterUpgradingKey]) > 0
}

func (sm *StatusManager) masterUpgraded() bool {
	sm.f.lock.Lock()
	defer sm.f.lock.Unlock()

	return len(sm.cm.Data[masterUpgradedKey]) > 0
}

func (sm *StatusManager) flush() {
	sm.f.lock.Lock()
	defer sm.f.lock.Unlock()

	klog.V(3).Infof("Attempting to flush %v", sm.cm.Data)

	// If master is in the process of upgrading, we stop the informer.
	if sm.informerRunning && len(sm.cm.Data[masterUpgradingKey]) > 0 {
		sm.stopInformer()
	}

	// Restart ConfigMap informer if it was previously shut down
	if !sm.informerRunning && len(sm.cm.Data[masterUpgradedKey]) > 0 {
		klog.V(2).Infof("Master has successfully upgraded at %s", sm.cm.Data[masterUpgradedKey])
		sm.startInformer()
	}

	// K8s considers its version of the ConfigMap to be latest, so we must get
	// the configmap from k8s first.
	updatedCm, err := sm.f.Clientset.CoreV1().ConfigMaps("default").Get(configMapName, metav1.GetOptions{})
	// The k8s API returns an empty ConfigMap upon error - we return early in
	// order to not overwrite our ConfigMap data.
	if err != nil {
		// if the k8s master is upgrading, we suppress the error message because
		// we expect a "connection refused" error in this situation.
		if len(sm.cm.Data[masterUpgradingKey]) > 0 {
			return
		}

		klog.Warningf("Error getting ConfigMap: %v", err)
		return
	}

	if updatedCm.Data == nil {
		updatedCm.Data = make(map[string]string)
	}

	// We give precedence to the master-upgraded and master-upgrading flags
	// set by the external test framework, but otherwise we prioritize
	// Ingress statuses set by StatusManager.
	for key, value := range sm.cm.Data {
		if key != masterUpgradedKey && key != masterUpgradingKey {
			updatedCm.Data[key] = value
		}
	}
	sm.cm = updatedCm
	sm.cm.Name = configMapName

	_, err = sm.f.Clientset.CoreV1().ConfigMaps("default").Update(sm.cm)
	if err != nil {
		klog.Warningf("Error updating ConfigMap: %v", err)
	} else {
		klog.V(3).Infof("Flushed statuses %v to ConfigMap", sm.cm.Data)
	}
}
