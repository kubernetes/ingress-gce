/*
Copyright 2026 The Kubernetes Authors.

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

package resourcemanager

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// ensureNetworkEndpointGroup ensures corresponding NEG is configured correctly in the specified zone.
func ensureNetworkEndpointGroup(svcNamespace, svcName, negName, zone, negServicePortName, kubeSystemUID, port string, networkEndpointType negtypes.NetworkEndpointType, cloud negtypes.NetworkEndpointGroupCloud, serviceLister cache.Indexer, recorder record.EventRecorder, version meta.Version, customName bool, networkInfo network.NetworkInfo, logger klog.Logger, negMetrics *metrics.NegMetrics) (*composite.NetworkEndpointGroup, error) {
	negLogger := logger.WithValues("negName", negName, "zone", zone)
	neg, err := cloud.GetNetworkEndpointGroup(negName, zone, version, logger)
	if err != nil {
		if !utils.IsNotFoundError(err) {
			negLogger.Error(err, "Failed to get Neg")
			return nil, err
		}
		negLogger.Info("Neg was not found", "err", err)
		negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}

	needToCreate := false
	if neg == nil {
		needToCreate = true
	} else {
		expectedDesc := utils.NegDescription{
			ClusterUID:  kubeSystemUID,
			Namespace:   svcNamespace,
			ServiceName: svcName,
			Port:        port,
		}
		if customName && neg.Description == "" {
			negLogger.Error(nil, "Found Neg with custom name but empty description")
			return nil, fmt.Errorf("found a custom named neg %s with an empty description", negName)
		}
		if matches, err := utils.VerifyDescription(expectedDesc, neg.Description, negName, zone); !matches {
			negLogger.Error(err, "Neg Name is already in use")
			// Wrap returned error from VerifyDescription() since we need to check if error is ErrNEGUsedByAnotherSyncer.
			return nil, fmt.Errorf("found conflicting description in neg %s: %w", negName, err)
		}

		if networkEndpointType != negtypes.NonGCPPrivateEndpointType &&
			// Only perform the following checks when the NEGs are not Non-GCP NEGs.
			// Non-GCP NEGs do not have associated network and subnetwork.
			(!utils.EqualResourceIDs(neg.Network, networkInfo.NetworkURL) ||
				!utils.EqualResourceIDs(neg.Subnetwork, networkInfo.SubnetworkURL)) {

			needToCreate = true
			negLogger.Info("NEG does not match network and subnetwork of the cluster. Deleting NEG")
			err = cloud.DeleteNetworkEndpointGroup(negName, zone, version, logger)
			if err != nil {
				return nil, err
			}
			if recorder != nil && serviceLister != nil {
				if svc := getService(serviceLister, svcNamespace, svcName, logger, negMetrics); svc != nil {
					recorder.Eventf(svc, apiv1.EventTypeNormal, "Delete", "Deleted NEG %q for %s in %q.", negName, negServicePortName, zone)
				}
			}
		}
	}

	if needToCreate {
		var subnetwork string
		switch networkEndpointType {
		case negtypes.NonGCPPrivateEndpointType:
			subnetwork = ""
		default:
			subnetwork = networkInfo.SubnetworkURL
		}
		negLogger.Info("Creating NEG", "negServicePortName", negServicePortName, "network", networkInfo.NetworkURL, "subnetwork", subnetwork)
		desc := ""
		negDesc := utils.NegDescription{
			ClusterUID:  kubeSystemUID,
			Namespace:   svcNamespace,
			ServiceName: svcName,
			Port:        port,
		}
		desc = negDesc.String()

		err = cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Version:             version,
			Name:                negName,
			NetworkEndpointType: string(networkEndpointType),
			Network:             networkInfo.NetworkURL,
			Subnetwork:          subnetwork,
			Description:         desc,
		}, zone, logger)
		if err != nil {
			return nil, err
		}
		if recorder != nil && serviceLister != nil {
			if svc := getService(serviceLister, svcNamespace, svcName, logger, negMetrics); svc != nil {
				recorder.Eventf(svc, apiv1.EventTypeNormal, "Create", "Created NEG %q for %s in %q.", negName, negServicePortName, zone)
			}
		}
	}

	if neg == nil {
		var err error
		neg, err = cloud.GetNetworkEndpointGroup(negName, zone, version, logger)
		if err != nil {
			negLogger.Error(err, "Error while retrieving NEG after initialization")
			return nil, err
		}
	}

	return neg, nil
}

// getService retrieves service object from serviceLister based on the input Namespace and Name
func getService(serviceLister cache.Indexer, namespace, name string, logger klog.Logger, negMetrics *metrics.NegMetrics) *apiv1.Service {
	if serviceLister == nil {
		return nil
	}
	service, exists, err := serviceLister.GetByKey(utils.ServiceKeyFunc(namespace, name))
	if exists && err == nil {
		return service.(*apiv1.Service)
	}
	if err != nil {
		logger.Error(err, "Failed to retrieve service from store", "namespace", namespace, "name", name)
		negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}
	return nil
}
