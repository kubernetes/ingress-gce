/*
Copyright 2021 The Kubernetes Authors.
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

package namer

import (
	"fmt"
	"strings"

	"k8s.io/ingress-gce/pkg/utils/common"
)

const (
	// maxSADescriptiveLabel is the max length for prefix, namespace, and name for
	// service attachment. 63 - 1 (naming schema version prefix)
	// - 2 (service attachment identifier prefix) - 8 (truncated kube system id) - 8 (suffix hash)
	// - 5 (hyphen connectors) = 39
	maxSADescriptiveLabel = 39

	// serviceAttachmentPrefix is the prefix used in service attachment naming scheme
	serviceAttachmentPrefix = "sa"
)

// V1ServiceAttachment implements ServiceAttachmentNamer. This is a wrapper on top of namer.Namer.
type V1ServiceAttachmentNamer struct {
	namer         *Namer
	kubeSystemUID string
	prefix        string

	// maxDescriptiveLabel is the max length for the namespace and name fields in the service
	// attachment name.
	// maxSADescriptiveLabel - len(prefix)
	maxDescriptiveLabel int
}

// NewServiceAttachmentNamer returns a v1 namer for Service Attachments
func NewServiceAttachmentNamer(namer *Namer, kubeSystemUID string) ServiceAttachmentNamer {
	return &V1ServiceAttachmentNamer{
		namer:               namer,
		kubeSystemUID:       kubeSystemUID,
		prefix:              namer.prefix,
		maxDescriptiveLabel: maxSADescriptiveLabel - len(namer.prefix),
	}
}

// ServiceAttachment returns the gce ServiceAttachment name based on the
// service attachment name, and namespace. Service Attachment naming convention:
//
// k8s{naming version}-sa-{cluster-uid}-{namespace}-{name}-{hash}
// Output name is at most 63 characters.
// Hash is generated from the KubeSystemUID, Namespace, Name, and Service Attachment UID
// Cluster UID will be 8 characters, hash suffix will be 8 characters
//
// WARNING: Controllers will use the naming convention to correlate between
// the Service Attachment CR and service attachment resource in GCE,
// so modifications must be backwards compatible.
func (n *V1ServiceAttachmentNamer) ServiceAttachment(namespace, name, saUID string) string {
	clusterUID := common.ContentHash(n.kubeSystemUID, clusterUIDLength)
	hash := n.suffix(8, n.kubeSystemUID, namespace, name, saUID)
	truncFields := TrimFieldsEvenly(n.maxDescriptiveLabel, namespace, name)
	return fmt.Sprintf("%s%s-sa-%s-%s-%s-%s", n.prefix, schemaVersionV1, clusterUID, truncFields[0], truncFields[1], hash)
}

// hash returns an 8 character hash code of the provided fields
func (n *V1ServiceAttachmentNamer) suffix(numCharacters int, fields ...string) string {
	concatenatedString := strings.Join(fields, ";")
	return common.ContentHash(concatenatedString, numCharacters)
}
