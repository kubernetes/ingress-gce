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

package negstatushandler

import (
	"k8s.io/client-go/tools/cache"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
)

// TestSvcNegStatusHandler wraps SvcNegStatusHandler to expose SvcNeg client and lister for test purposes.
type TestSvcNegStatusHandler struct {
	*SvcNegStatusHandler
}

// NewTestSvcNegStatusHandler wraps a concrete SvcNegStatusHandler inside the test handler subclass.
func NewTestSvcNegStatusHandler(handler *SvcNegStatusHandler) *TestSvcNegStatusHandler {
	return &TestSvcNegStatusHandler{handler}
}

// SvcNEGClient exposes the unexported svcNEGClient field for test assertions.
func (h *TestSvcNegStatusHandler) SvcNEGClient() svcnegclient.Interface {
	return h.svcNEGClient
}

// SvcNEGLister exposes the unexported svcNEGLister field for test assertions.
func (h *TestSvcNegStatusHandler) SvcNEGLister() cache.Indexer {
	return h.svcNEGLister
}
