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

package fuzz

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var baseIngress = &v1beta1.Ingress{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "ing1",
		Namespace: "default",
	},
	Status: v1beta1.IngressStatus{
		LoadBalancer: v1.LoadBalancerStatus{
			Ingress: []v1.LoadBalancerIngress{
				{IP: "127.0.0.1"},
			},
		},
	},
}

const (
	mockValidatorOk = iota
	mockValidatorUnstable
	mockValidatorHTTPS
	mockValidatorConfigureError
	mockValidatorCheckError
	mockValidatorSkipCheck
)

type mockFeature struct {
	NullValidator
	mode int
}

func (m *mockFeature) Name() string {
	return "MockFeature"
}

func (m *mockFeature) NewValidator() FeatureValidator {
	return m
}

func (m *mockFeature) ConfigureAttributes(env ValidatorEnv, ing *v1beta1.Ingress, a *IngressValidatorAttributes) error {
	switch m.mode {
	case mockValidatorUnstable:
		a.CheckHTTP = !a.CheckHTTP
	case mockValidatorHTTPS:
		a.CheckHTTPS = true
	case mockValidatorConfigureError:
		glog.Infof("Injected ConfigureAttributes error")
		return errors.New("injected error")
	}
	return nil
}

func (m *mockFeature) CheckResponse(host, path string, resp *http.Response, body []byte) (CheckResponseAction, error) {
	switch m.mode {
	case mockValidatorCheckError:
		glog.Infof("Injected CheckResponse error")
		return CheckResponseContinue, errors.New("injected error")
	case mockValidatorSkipCheck:
		return CheckResponseSkip, nil
	}
	return CheckResponseContinue, nil
}

func TestIngressValidatorAttributes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc           string
		ing            *v1beta1.Ingress
		wantCheckHTTP  bool
		wantCheckHTTPS bool
	}{
		{
			desc:          "default",
			ing:           NewIngressBuilder("ns1", "name1", "").I,
			wantCheckHTTP: true,
		},
		{
			desc:           "with TLS",
			ing:            NewIngressBuilder("ns1", "name1", "").AddTLS([]string{"foo.com"}, "s1").I,
			wantCheckHTTP:  true,
			wantCheckHTTPS: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			a := &IngressValidatorAttributes{}
			a.baseAttributes(tc.ing)
			if a.CheckHTTP != tc.wantCheckHTTP {
				t.Errorf("a.CheckHTTP = %t, want %t", a.CheckHTTP, tc.wantCheckHTTP)
			}
			if a.CheckHTTPS != tc.wantCheckHTTPS {
				t.Errorf("a.CheckHTTPS = %t, want %t", a.CheckHTTPS, tc.wantCheckHTTPS)
			}
		})
	}
}

func TestNewIngressValidator(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc     string
		ing      *v1beta1.Ingress
		features []Feature

		wantErr bool
	}{
		{
			desc: "ok",
			ing:  baseIngress,
		},
		{
			desc:     "set HTTPS",
			ing:      baseIngress,
			features: []Feature{&mockFeature{mode: mockValidatorHTTPS}},
		},
		{
			desc:     "unstable feature",
			ing:      baseIngress,
			features: []Feature{&mockFeature{mode: mockValidatorUnstable}},
			wantErr:  true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := NewIngressValidator(&StaticValidatorEnv{}, tc.ing, tc.features, nil)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("NewIngressValidator() = %v; gotErr = %t, wantErr =%t", err, gotErr, tc.wantErr)
			}
		})
	}
}

type mockServer struct {
	l                 net.Listener
	ls                net.Listener
	s                 http.Server
	hasDefaultBackend bool

	lock        sync.Mutex
	reqsForPath map[string]int
}

func (m *mockServer) listen() error {
	var err error
	m.l, err = net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	glog.V(2).Infof("HTTP is listening on %s", m.l.Addr())

	m.ls, err = net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	glog.V(2).Infof("HTTPS is listening on %s", m.ls.Addr())

	return nil
}

func (m *mockServer) serve() {
	m.reqsForPath = map[string]int{}
	okFunc := func(w http.ResponseWriter, r *http.Request) {
		m.lock.Lock()
		defer m.lock.Unlock()

		m.reqsForPath[r.URL.Path] = m.reqsForPath[r.URL.Path] + 1
		w.Write([]byte("ok"))
	}
	notFoundFunc := func(w http.ResponseWriter, r *http.Request) {
		m.lock.Lock()
		defer m.lock.Unlock()

		m.reqsForPath[r.URL.Path] = m.reqsForPath[r.URL.Path] + 1
		w.WriteHeader(404)
	}

	var mux http.ServeMux
	if m.hasDefaultBackend {
		mux.HandleFunc("/", okFunc)
	} else {
		mux.HandleFunc("/", notFoundFunc)
	}
	mux.HandleFunc("/path1", okFunc)
	mux.HandleFunc("/path2", okFunc)
	mux.HandleFunc("/path3", okFunc)
	m.s.Handler = &mux

	go m.s.Serve(m.l)
	go m.s.ServeTLS(m.ls, "test-cert.pem", "test-key.pem")
}

func TestValidatorCheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress

		dontStartServer   bool
		hasDefaultBackend bool
		wantErr           bool
		wantPaths         []string
	}{
		{
			desc: "simple",
			ing:  NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).I,
		},
		{
			desc:              "default backend",
			ing:               NewIngressBuilderFromExisting(baseIngress).DefaultBackend("s", port80).I,
			hasDefaultBackend: true,
		},
		{
			desc: "multiple paths",
			ing:  NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).AddPath("test.com", "/path2", "s", port80).AddPath("test.com", "/path3", "s", port80).I,
		},
		{
			desc: "TLS",
			ing:  NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).AddTLS([]string{"test.com"}, "secret1").I,
		},
		{
			desc:    "no VIP",
			ing:     NewIngressBuilder("ns1", "ing1", "").AddPath("test.com", "/badpath", "s", port80).I,
			wantErr: true,
		},
		{
			desc:    "bad paths",
			ing:     NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/badpath", "s", port80).I,
			wantErr: true,
		},
		{
			desc:    "bad paths TLS",
			ing:     NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/badpath", "s", port80).AddTLS([]string{"test.com"}, "secret1").I,
			wantErr: true,
		},
		{
			desc:            "server timeout",
			ing:             NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/badpath", "s", port80).AddTLS([]string{"test.com"}, "secret1").I,
			dontStartServer: true,
			wantErr:         true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ms := mockServer{hasDefaultBackend: tc.hasDefaultBackend}
			if err := ms.listen(); err != nil {
				t.Fatalf("ms.listen() = %v, want nil", err)
			}
			if !tc.dontStartServer {
				ms.serve()
			}

			attribs := defaultAttributes()
			attribs.HTTPPort = ms.l.Addr().(*net.TCPAddr).Port
			attribs.HTTPSPort = ms.ls.Addr().(*net.TCPAddr).Port
			validator, err := NewIngressValidator(&StaticValidatorEnv{}, tc.ing, []Feature{}, attribs)
			if err != nil {
				t.Fatalf("NewIngressValidator(...) = _, %v; want _, nil", err)
			}
			err = validator.Check(ctx).Err
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("validator.Check(ctx) = %v; gotErr = %t, wantErr = %t", err, gotErr, tc.wantErr)
			}
			// Check that the server received requests for all paths.
			for _, p := range tc.wantPaths {
				if _, ok := ms.reqsForPath[p]; !ok {
					t.Errorf("did not receive a request on path %q", p)
				}
			}
		})
	}
}

func TestValidatorCheckFeature(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc    string
		ing     *v1beta1.Ingress
		feature Feature

		wantNewValidatorErr bool
		wantErr             bool
	}{
		{
			desc:    "simple",
			ing:     NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).I,
			feature: &mockFeature{},
		},
		{
			desc:    "skip default check",
			ing:     NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).I,
			feature: &mockFeature{mode: mockValidatorSkipCheck},
		},
		{
			desc:                "error in configure",
			ing:                 NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).I,
			feature:             &mockFeature{mode: mockValidatorConfigureError},
			wantNewValidatorErr: true,
		},
		{
			desc:    "error in check",
			ing:     NewIngressBuilderFromExisting(baseIngress).AddPath("test.com", "/path1", "s", port80).I,
			feature: &mockFeature{mode: mockValidatorCheckError},
			wantErr: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var ms mockServer
			if err := ms.listen(); err != nil {
				t.Fatalf("ms.listen() = %v, want nil", err)
			}
			ms.serve()

			attribs := defaultAttributes()
			attribs.HTTPPort = ms.l.Addr().(*net.TCPAddr).Port
			attribs.HTTPSPort = ms.ls.Addr().(*net.TCPAddr).Port

			validator, err := NewIngressValidator(&StaticValidatorEnv{}, tc.ing, []Feature{tc.feature}, attribs)
			if gotErr := err != nil; gotErr != tc.wantNewValidatorErr {
				t.Errorf("NewIngressValidator(...) = _, %v; gotErr = %t, want %t", err, gotErr, tc.wantNewValidatorErr)
			}
			if err != nil {
				return
			}

			err = validator.Check(ctx).Err
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("validator.Check(ctx) = %v; gotErr = %t, wantErr = %t", err, gotErr, tc.wantErr)
			}
		})
	}
}

func TestPortStr(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc   string
		scheme string
		a      IngressValidatorAttributes
		want   string
	}{
		{
			desc:   "http, default port",
			scheme: "http",
			a:      *defaultAttributes(),
			want:   "",
		},
		{
			desc:   "https, default port",
			scheme: "https",
			a:      *defaultAttributes(),
			want:   "",
		},
		{
			desc:   "http, custom port",
			scheme: "http",
			a:      IngressValidatorAttributes{HTTPPort: 8080},
			want:   ":8080",
		},
		{
			desc:   "https, custom port",
			scheme: "https",
			a:      IngressValidatorAttributes{HTTPSPort: 8443},
			want:   ":8443",
		},
		{
			desc:   "invalid scheme",
			scheme: "invalid",
			want:   "",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := portStr(&tc.a, tc.scheme); got != tc.want {
				t.Errorf("portStr(%+v, %q) = %q, want %q", tc.a, tc.scheme, got, tc.want)
			}
		})
	}
}
