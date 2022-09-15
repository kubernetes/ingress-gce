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

package events

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

const (
	AddNodes    = "IngressGCE_AddNodes"
	RemoveNodes = "IngressGCE_RemoveNodes"

	SyncIngress       = "Sync"
	TranslateIngress  = "Translate"
	IPChanged         = "IPChanged"
	GarbageCollection = "GarbageCollection"

	SyncService = "Sync"
)

type RecorderProducer interface {
	Recorder(ns string) record.EventRecorder
}

type RecorderProducerMock struct {
}

func (r RecorderProducerMock) Recorder(ns string) record.EventRecorder {
	return &record.FakeRecorder{}
}

// GlobalEventf records a Cluster level event not attached to a given object.
func GlobalEventf(r record.EventRecorder, eventtype, reason, messageFmt string, args ...interface{}) {
	// Using an empty ObjectReference to indicate no associated
	// resource. This apparently works, see the package
	// k8s.io/client-go/tools/record.
	r.Eventf(&v1.ObjectReference{}, eventtype, reason, messageFmt, args...)
}

// truncatedStringListMax is a variable to make testing easier. This
// value should not be modified.
var truncatedStringListMax = 2000

// TruncateStringList will render the list of items as a string,
// eliding elements with ellipsis at the end if there are more than a
// reasonable number of characters in the resulting string. This is
// used to prevent accidentally dumping enormous strings into the
// Event description.
func TruncatedStringList(items []string) string {
	var (
		ret   = "["
		first = true
	)
	for _, s := range items {
		if len(ret)+len(s)+1 > truncatedStringListMax {
			ret += ", ..."
			break
		}
		if !first {
			ret += ", "
		}
		first = false
		ret += s
	}
	return ret + "]"
}
