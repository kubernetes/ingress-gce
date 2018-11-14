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
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
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
	// masterUpgraded is the key used to indicate to the status manager that
	// the k8s master has successfully finished upgrading.
	// Value associated with it is a timestamp string.
	masterUpgraded = "master-upgraded"
)

// StatusManager manages the status of sandboxed Ingresses via a ConfigMap.
// Upon initialization, it creates a ConfigMap object which the guitar
// test can also read and write to.
// Ingress e2e tests write to the ConfigMap whether or not the Ingresses
// created have stabilized or not. The Guitar test reads from this same
// configmap and writes to the master-upgraded key indicating that a k8s master
// upgrade has successfully finished, and exit key to indicate that the e2e test
// can exit.
type StatusManager struct {
	cm *v1.ConfigMap
	f  *Framework
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
	sm.cm, err = sm.f.Clientset.Core().ConfigMaps("default").Create(sm.cm)
	if err != nil {
		return fmt.Errorf("error creating ConfigMap: %v", err)
	}

	newIndexer := func() cache.Indexers {
		return cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	}
	cmInformer := informerv1.NewConfigMapInformer(sm.f.Clientset, "default", cmPollInterval, newIndexer())
	cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, cur interface{}) {
			curCm := cur.(*v1.ConfigMap)
			if len(curCm.Data[exitKey]) > 0 {
				glog.V(2).Infof("ConfigMap was updated with exit switch at %s", curCm.Data[exitKey])
				sm.f.shutdown(0)
			}
		},
	})

	go func() {
		for _ = range time.NewTicker(flushInterval).C {
			sm.flush()
		}
	}()

	return nil
}

func (sm *StatusManager) shutdown() {
	glog.V(2).Infof("Shutting down status manager.")
	glog.V(3).Infof("ConfigMap: %+v", sm.cm.Data)
	if err := sm.f.Clientset.Core().ConfigMaps("default").Delete(configMapName, &metav1.DeleteOptions{}); err != nil {
		glog.Errorf("Error deleting ConfigMap: %v", err)
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

func (sm *StatusManager) masterUpgraded() bool {
	if len(sm.cm.Data[masterUpgraded]) > 0 {
		glog.V(2).Infof("Master has successfully upgraded at %s", sm.cm.Data[masterUpgraded])
		return true
	}
	return false
}

func (sm *StatusManager) flush() {
	sm.f.lock.Lock()
	defer sm.f.lock.Unlock()

	// Loop until we successfully update the config map
	for {
		var err error
		sm.cm, err = sm.f.Clientset.Core().ConfigMaps("default").Update(sm.cm)
		if err != nil {
			glog.Errorf("Error updating ConfigMap: %v", err)
		} else {
			// ConfigMap successfully updated
			break
		}
	}
	glog.V(3).Infof("Flushed statuses to ConfigMap")
	glog.V(3).Infof("ConfigMap: %+v", sm.cm.Data)
}
