/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog"
)

const (
	podNotTrackedErrTemplate = "pod %q is not tracked"
	negNotTrackedErrTemplate = "neg %q is not tracked"
)

// objKey is the key identify a k8s obj
type objKey struct {
	types.NamespacedName
}

func newObjKey(namespace, name string) objKey {
	return objKey{types.NamespacedName{Namespace: namespace, Name: name}}
}

// negName is the name of Negs associated with a service port
type negName string

func (n negName) String() string {
	return string(n)
}

func getNegNameList(negs []string) []negName {
	ret := make([]negName, len(negs))
	for i, neg := range negs {
		ret[i] = negName(neg)
	}
	return ret
}

// podInfo contains information assocatied with pod
type podInfo struct {
	IP   string
	Port string
	Zone string
}

type endpointKey struct {
	IP   string
	Port string
}

type endpointInfoMap map[endpointKey]objKey

// podInfoMap maps a pod to its info
type podInfoMap map[objKey]podInfo

// svcInfo contains the information associated with service
type svcInfo struct {
	Negs []negName
}

func (info svcInfo) GetNegNameStrings() []string {
	ret := make([]string, len(info.Negs))
	for i := range info.Negs {
		ret[i] = info.Negs[i].String()
	}
	return ret
}

func newSvcInfo(negStrings []string) svcInfo {
	ret := svcInfo{}
	ret.Negs = make([]negName, len(negStrings))

	for i := range negStrings {
		ret.Negs[i] = negName(negStrings[i])
	}
	return ret
}

// serviceInfoMap maps service to service Info
// namespace -> name -> svcInfo
type serviceInfoMap map[string]map[string]svcInfo

func (m serviceInfoMap) GetSvcInfo(namespace, name string) (svcInfo, bool) {
	if svcMap, nsFound := m[namespace]; nsFound {
		ret, svcFound := svcMap[name]
		return ret, svcFound
	}
	return svcInfo{}, false
}

func (m serviceInfoMap) SetSvcInfo(namespace, name string, info svcInfo) {
	if _, nsFound := m[namespace]; !nsFound {
		m[namespace] = make(map[string]svcInfo)
	}
	m[namespace][name] = info
}

func (m serviceInfoMap) RemoveSvcInfo(namespace, name string) error {
	_, ok := m[namespace]
	if !ok {
		return fmt.Errorf("namespace %q not found", namespace)
	}
	_, ok = m[namespace][name]
	if !ok {
		return fmt.Errorf("service %v/%v not found", namespace, name)
	}
	delete(m[namespace], name)
	return nil
}

func (m serviceInfoMap) GetSvcsInNamespace(namespace string) []string {
	if svcMap, nsFound := m[namespace]; nsFound {
		ret := make([]string, len(m[namespace]))
		i := 0
		for svcName := range svcMap {
			ret[i] = svcName
			i++
		}
		return ret
	} else {
		return []string{}
	}
}

// podHealthStatusMap maps pod to its corresponding NEG health status
// Pod -> Negs -> HealthStatus (bool)
type podHealthStatusMap map[objKey]map[negName]bool

// negPodInfoMap maps neg to pods and podInfos
type negPodInfoMap map[negName]podInfoMap

// podNegTracker provides data structure and methods to track Negs, Pods, NEG-Pod/Pod-NEG mapping and additional metadata.
type podNegTracker struct {
	lock sync.Mutex
	// podNegMap maps a pod to the Negs it belongs to and the corresponding health status in each NEG.
	// Pod -> Negs -> HealthStatus
	podNegMap podHealthStatusMap
	// negPodMap maps NEG groups the pods.
	// NEG -> Pods -> Zone/IP
	negPodMap negPodInfoMap
}

func newNegTracker() *podNegTracker {
	return &podNegTracker{
		podNegMap: podHealthStatusMap{},
		negPodMap: negPodInfoMap{},
	}
}

// AddNeg starts tracking a neg
func (t *podNegTracker) AddNeg(neg negName) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.negPodMap[neg] = podInfoMap{}
}

// GetPodsInNeg returns all tracked pods in a neg
func (t *podNegTracker) GetPodsInNeg(neg negName) ([]objKey, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()
	var ret []objKey
	podMap, ok := t.negPodMap[neg]
	if ok {
		ret = make([]objKey, len(podMap))
		i := 0
		for key := range podMap {
			ret[i] = key
			i++
		}
	}
	return ret, ok
}

// GetUnhealthyEndpointsByZone returns the pods in a neg and group them by zone
func (t *podNegTracker) GetUnhealthyEndpointsByZone(neg negName) (map[string]endpointInfoMap, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()
	podMap, ok := t.negPodMap[neg]
	if !ok {
		return nil, false
	}
	ret := map[string]endpointInfoMap{}
	for key, info := range podMap {
		if _, ok := ret[info.Zone]; !ok {
			ret[info.Zone] = endpointInfoMap{}
		}
		if healthy, _ := t.getPodNegHealthStatus(key, neg); healthy {
			ret[info.Zone][endpointKey{IP: info.IP, Port: info.Port}] = key
		}
	}
	return ret, true
}

// RemoveNeg removes all traces for the NEG
func (t *podNegTracker) RemoveNeg(neg negName) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if err := t.validateNeg(neg); err != nil {
		return err
	}

	errList := []error{}
	for pod := range t.negPodMap[neg] {
		if err := t.validatePod(pod); err != nil {
			errList = append(errList, err)
			continue
		}
		delete(t.podNegMap[pod], neg)
	}
	delete(t.negPodMap, neg)
	return utilerrors.NewAggregate(errList)
}

func (t *podNegTracker) AddPodToNegs(pod objKey, negs []negName) error {
	klog.V(2).Infof("====================AddPodToNegs(%v, %v)", pod, negs)
	t.lock.Lock()
	defer t.lock.Unlock()
	// validate
	errList := []error{}
	for _, neg := range negs {
		if err := t.validateNeg(neg); err != nil {
			errList = append(errList, err)
			continue
		}

		_, podFound := t.negPodMap[neg][pod]
		if podFound {
			errList = append(errList, fmt.Errorf("pod %q is already tracked in neg %q", pod.String(), neg.String()))
		}
	}
	if len(errList) > 0 {
		return utilerrors.NewAggregate(errList)
	}

	if !t.podIsTracked(pod) {
		t.podNegMap[pod] = make(map[negName]bool)
	}

	// persist the input into internal struct
	for _, neg := range negs {
		t.podNegMap[pod][neg] = false
		t.negPodMap[neg][pod] = podInfo{}
	}
	return nil
}

func (t *podNegTracker) RemovePod(pod objKey) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if err := t.validatePod(pod); err != nil {
		return err
	}

	errList := []error{}
	for neg := range t.podNegMap[pod] {
		if err := t.validateNeg(neg); err != nil {
			errList = append(errList, err)
			continue
		}
		delete(t.negPodMap[neg], pod)
	}
	delete(t.podNegMap, pod)
	return utilerrors.NewAggregate(errList)
}

func (t *podNegTracker) GetPodHealthStatus(key objKey) (map[negName]bool, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()
	healthStatusMap, ok := t.getPodHealthStatus(key)
	ret := map[negName]bool{}
	if ok {
		for k, v := range healthStatusMap {
			ret[k] = v
		}
	}
	return ret, ok
}

func (t *podNegTracker) getPodHealthStatus(key objKey) (map[negName]bool, bool) {
	healthStatusMap, ok := t.podNegMap[key]
	return healthStatusMap, ok
}

func (t *podNegTracker) getPodNegHealthStatus(key objKey, neg negName) (bool, bool) {
	healthStatusMap, ok := t.getPodHealthStatus(key)
	ret := false
	negFound := false
	if ok {
		ret, negFound = healthStatusMap[neg]
	}
	return ret, negFound
}

func (t *podNegTracker) UpdatePodInfo(pod objKey, neg negName, zone, ip, port string) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if err := t.validatePodInNeg(pod, neg); err != nil {
		return err
	}
	t.negPodMap[neg][pod] = podInfo{Zone: zone, IP: ip, Port: port}
	return nil
}

func (t *podNegTracker) UpdatePodHealthStatus(pod objKey, neg negName, healthStatus bool) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if err := t.validatePodInNeg(pod, neg); err != nil {
		return err
	}
	t.podNegMap[pod][neg] = true
	return nil
}

// PodIsTracked returns if the pod is tracked
func (t *podNegTracker) PodIsTracked(key objKey) bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.podIsTracked(key)
}

func (t *podNegTracker) podIsTracked(key objKey) bool {
	_, ok := t.podNegMap[key]
	return ok
}

// PodIsInNeg returns if the pod is tracked
func (t *podNegTracker) PodIsInNeg(key objKey, neg negName) bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	negMap, ok := t.podNegMap[key]
	if !ok {
		return false
	}
	_, ok = negMap[neg]
	return ok
}

func (t *podNegTracker) validatePod(pod objKey) error {
	if _, ok := t.podNegMap[pod]; !ok {
		return fmt.Errorf(podNotTrackedErrTemplate, pod.String())
	}
	return nil
}

func (t *podNegTracker) validateNeg(neg negName) error {
	if _, ok := t.negPodMap[neg]; !ok {
		return fmt.Errorf(negNotTrackedErrTemplate, neg.String())
	}
	return nil
}

func (t *podNegTracker) validatePodInNeg(pod objKey, neg negName) error {
	errList := []error{}
	if err := t.validatePod(pod); err != nil {
		errList = append(errList, err)
	}
	if err := t.validateNeg(neg); err != nil {
		errList = append(errList, err)
	}
	if len(errList) == 0 {
		_, ok := t.negPodMap[neg][pod]
		if !ok {
			errList = append(errList, fmt.Errorf("Pod %q is not tracked as part of NEG %q", pod.String(), neg.String()))
		}

		_, ok = t.podNegMap[pod][neg]
		if !ok {
			errList = append(errList, fmt.Errorf("NEG %q is not tracked as part of pod %q", neg.String(), pod.String()))
		}
	}
	return utilerrors.NewAggregate(errList)
}

func (t *podNegTracker) validateNegs(negs []negName) error {
	var errList []error
	for _, neg := range negs {
		if err := t.validateNeg(neg); err != nil {
			errList = append(errList, err)
		}
	}
	return utilerrors.NewAggregate(errList)
}
