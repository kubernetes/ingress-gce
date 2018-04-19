// Copyright 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package target

import (
	"reflect"

	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

// objectMetaEquivalent checks if cluster-independent, user provided data in two given ObjectMeta are equal.
// If in the future the ObjectMeta structure is expanded then any field that is not populated
// by the api server should be included here.
// Ignores annotations with keys in ignoreAnnotationKeys.
// Note: copied from https://github.com/kubernetes/federation/blob/7951a643cebc3abdcd903eaff90d1383b43928d1/pkg/federation-controller/util/meta.go#L61
func objectMetaEquivalent(a, b meta_v1.ObjectMeta, ignoreAnnotationKeys map[string]string) bool {
	if a.Name != b.Name {
		return false
	}
	if a.Namespace != b.Namespace {
		return false
	}
	if !reflect.DeepEqual(a.Labels, b.Labels) && (len(a.Labels) != 0 || len(b.Labels) != 0) {
		return false
	}
	// Remove ignoreAnnotationKeys from the list of annotations.
	aAnnotations := utils.PruneMap(a.Annotations, ignoreAnnotationKeys)
	bAnnotations := utils.PruneMap(b.Annotations, ignoreAnnotationKeys)
	if !reflect.DeepEqual(aAnnotations, bAnnotations) && (len(aAnnotations) != 0 || len(bAnnotations) != 0) {
		return false
	}
	return true
}

// objectMetaAndSpecEquivalent checks if cluster-independent, user provided data in ObjectMeta
// and Spec in two given top level api objects are equivalent.
// Ignores annotations with keys in ignoreAnnotationKeys.
// Note: copied from https://github.com/kubernetes/federation/blob/7951a643cebc3abdcd903eaff90d1383b43928d1/pkg/federation-controller/util/meta.go#L79
func objectMetaAndSpecEquivalent(a, b runtime.Object, ignoreAnnotationKeys map[string]string) bool {
	objectMetaA := reflect.ValueOf(a).Elem().FieldByName("ObjectMeta").Interface().(meta_v1.ObjectMeta)
	objectMetaB := reflect.ValueOf(b).Elem().FieldByName("ObjectMeta").Interface().(meta_v1.ObjectMeta)
	specA := reflect.ValueOf(a).Elem().FieldByName("Spec").Interface()
	specB := reflect.ValueOf(b).Elem().FieldByName("Spec").Interface()
	return objectMetaEquivalent(objectMetaA, objectMetaB, ignoreAnnotationKeys) && reflect.DeepEqual(specA, specB)
}

// createTargetIngress creates a copy of the passed in ingress but with certain
// modifications needed to make the target ingress viable in the target cluster.
func createTargetIngress(ing *v1beta1.Ingress) *v1beta1.Ingress {
	// We have to do a manual copy since doing a deep copy will
	// populate certain ObjectMeta fields we don't want.
	targetIng := &v1beta1.Ingress{}
	// Copy over name, namespace, annotations.
	targetIng.Name = ing.Name
	targetIng.Namespace = ing.Namespace
	targetIng.Annotations = ing.Annotations
	// Copy over spec.
	targetIng.Spec = ing.Spec
	// Add MCI annotation to "target" ingress.
	annotations.AddAnnotation(targetIng, annotations.IngressClassKey, annotations.GceMultiIngressClass)
	return targetIng
}

// updateTargetIng makes sure that targetIng is updated to reflect any changes in ing.
// Note that we only care about namespace, labels, and the spec. We do not care about any
// changes to feature annotations because the target ingress does not process them.
func updateTargetIng(ing *v1beta1.Ingress, targetIng *v1beta1.Ingress) {
	targetIng.Namespace = ing.Namespace
	targetIng.Labels = ing.Labels
	targetIng.Spec = ing.Spec
}
