/*
Copyright 2023 The Kubernetes Authors.

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

package healthchecks

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func NewFakeServiceGetter() ServiceGetter {
	return &fakeServiceGetter{}
}

type fakeServiceGetter struct{}

func (fsg *fakeServiceGetter) GetService(namespace, name string) (*v1.Service, error) {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
	}, nil
}

func NewFakeRecorderGetter(bufferSize int) RecorderGetter {
	return &fakeRecorderGetter{bufferSize}
}

type fakeRecorderGetter struct {
	bufferSize int
}

// Returns a different record.EventRecorder for every call.
func (frg *fakeRecorderGetter) Recorder(namespace string) record.EventRecorder {
	return record.NewFakeRecorder(frg.bufferSize)
}

type singletonFakeRecorderGetter struct {
	recorder *record.FakeRecorder
}

// Returns the same record.EventRecorder irrespective of the namespace.
func (sfrg *singletonFakeRecorderGetter) Recorder(namespace string) record.EventRecorder {
	return sfrg.FakeRecorder()
}

func (sfrg *singletonFakeRecorderGetter) FakeRecorder() *record.FakeRecorder {
	if sfrg.recorder == nil {
		panic("singletonFakeRecorderGetter not initialised: recorder is nil.")
	}
	return sfrg.recorder
}

func NewFakeSingletonRecorderGetter(bufferSize int) *singletonFakeRecorderGetter {
	return &singletonFakeRecorderGetter{recorder: record.NewFakeRecorder(bufferSize)}
}
