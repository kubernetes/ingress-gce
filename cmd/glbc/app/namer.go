/*
Copyright 2017 The Kubernetes Authors.

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

package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/storage"
)

const (
	// Key used to persist UIDs to configmaps.
	uidConfigMapName = "ingress-uid"
	// uidByteLength is the length in bytes for the random UID.
	uidByteLength = 8
)

// NewNamer returns a new naming policy given the state of the cluster.
func NewNamer(kubeClient kubernetes.Interface, clusterName, fwName string, logger klog.Logger) (*namer.Namer, error) {
	namer, err := NewStaticNamer(kubeClient, clusterName, fwName, logger)
	if err != nil {
		return nil, err
	}
	uidVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName, logger)

	// Start a goroutine to poll the cluster UID config map.  We don't
	// watch because we know exactly which configmap we want and this
	// controller already watches 5 other resources, so it isn't worth the
	// cost of another connection and complexity.
	go wait.Forever(func() {
		for _, key := range [...]string{storage.UIDDataKey, storage.ProviderDataKey} {
			val, found, err := uidVault.Get(key)
			if err != nil {
				logger.Error(err, "Can't read uidConfigMap", "uidConfigMapName", uidConfigMapName)
			} else if !found {
				errmsg := fmt.Sprintf("Can't read %v from uidConfigMap %v", key, uidConfigMapName)
				if key == storage.UIDDataKey {
					logger.Error(nil, errmsg)
				} else {
					logger.V(4).Info(errmsg)
				}
			} else {

				switch key {
				case storage.UIDDataKey:
					if uid := namer.UID(); uid != val {
						logger.Info("Cluster uid changed", "oldUid", uid, "newUid", val)
						namer.SetUID(val)
					}
				case storage.ProviderDataKey:
					if fw_name := namer.Firewall(); fw_name != val {
						logger.Info("Cluster firewall name changed", "oldFirewallName", fw_name, "newFirewallName", val)
						namer.SetFirewall(val)
					}
				}
			}
		}
	}, 5*time.Second)
	return namer, nil
}

// NewStaticNamer returns a new naming policy given a snapshot of cluster state. Note that this
// implementation does not dynamically change the naming policy based on changes in cluster state.
func NewStaticNamer(kubeClient kubernetes.Interface, clusterName, fwName string, logger klog.Logger) (*namer.Namer, error) {
	name, err := getClusterUID(kubeClient, clusterName, logger)
	if err != nil {
		return nil, err
	}
	fw_name, err := getFirewallName(kubeClient, fwName, name, logger)
	if err != nil {
		return nil, err
	}

	return namer.NewNamer(name, fw_name, logger), nil
}

// useDefaultOrLookupVault returns either a 'defaultName' or if unset, obtains
// a name from a ConfigMap.  The returned value follows this priority:
//
// If the provided 'defaultName' is not empty, that name is used.
//
//	This is effectively a client override via a command line flag.
//
// else, check cfgVault with 'configMapKey' as a key and if found, use the associated value
// else, return an empty 'name' and pass along an error iff the configmap lookup is erroneous.
func useDefaultOrLookupVault(cfgVault *storage.ConfigMapVault, configMapKey, defaultName string, logger klog.Logger) (string, error) {
	if defaultName != "" {
		logger.Info(fmt.Sprintf("Using user provided %v %v", configMapKey, defaultName))
		// Don't save the uid in the vault, so users can rollback
		// through setting the accompany flag to ""
		return defaultName, nil
	}
	val, found, err := cfgVault.Get(configMapKey)
	if err != nil {
		// This can fail because of:
		// 1. No such config map - found=false, err=nil
		// 2. No such key in config map - found=false, err=nil
		// 3. Apiserver flake - found=false, err!=nil
		// It is not safe to proceed in 3.
		return "", fmt.Errorf("failed to retrieve %v: %v, returning empty name", configMapKey, err)
	} else if !found {
		// Not found but safe to proceed.
		return "", nil
	}
	logger.Info(fmt.Sprintf("Using %v = %q saved in ConfigMap", configMapKey, val))
	return val, nil
}

// getFirewallName returns the firewall rule name to use for this cluster. For
// backwards compatibility, the firewall name will default to the cluster UID.
// Use getFlagOrLookupVault to obtain a stored or overridden value for the firewall name.
// else, use the cluster UID as a backup (this retains backwards compatibility).
func getFirewallName(kubeClient kubernetes.Interface, name, clusterUID string, logger klog.Logger) (string, error) {
	cfgVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName, logger)
	if firewallName, err := useDefaultOrLookupVault(cfgVault, storage.ProviderDataKey, name, logger); err != nil {
		return "", err
	} else if firewallName != "" {
		return firewallName, cfgVault.Put(storage.ProviderDataKey, firewallName, false)
	} else {
		logger.Info("Using cluster UID as firewall name", "clusterUID", clusterUID)
		return clusterUID, cfgVault.Put(storage.ProviderDataKey, clusterUID, false)
	}
}

// getClusterUID returns the cluster UID. Rules for UID generation:
// If the user specifies a --cluster-uid param it overwrites everything
// else, check UID config map for a previously recorded uid
// else, check if there are any working Ingresses
//   - remember that "" is the cluster uid
//
// else, allocate a new uid
func getClusterUID(kubeClient kubernetes.Interface, name string, logger klog.Logger) (string, error) {
	cfgVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName, logger)
	if name, err := useDefaultOrLookupVault(cfgVault, storage.UIDDataKey, name, logger); err != nil {
		return "", err
	} else if name != "" {
		return name, nil
	}

	// Check if the cluster has an Ingress with ip
	ings, err := kubeClient.NetworkingV1().Ingresses(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	})
	if err != nil {
		return "", err
	}
	namer := namer.NewNamer("", "", logger)
	for _, ing := range ings.Items {
		if len(ing.Status.LoadBalancer.Ingress) != 0 {
			c := namer.ParseName(loadbalancers.GCEResourceName(ing.Annotations, "forwarding-rule"))
			if c.ClusterName != "" {
				return c.ClusterName, cfgVault.Put(storage.UIDDataKey, c.ClusterName, false)
			}
			logger.Info("Found a working Ingress, assuming uid is empty string")
			return "", cfgVault.Put(storage.UIDDataKey, "", false)
		}
	}

	// Generate a random uid if it does not exist
	uid, err := randomUID()
	if err != nil {
		return "", err
	}
	return uid, cfgVault.Put(storage.UIDDataKey, uid, true)
}

func randomUID() (string, error) {
	b := make([]byte, uidByteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	uid := fmt.Sprintf("%x", b)
	return uid, nil
}
