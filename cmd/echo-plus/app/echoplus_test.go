package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthCheck(t *testing.T) {
	RegisterFlags()
	handler := router()
	server := httptest.NewServer(handler)
	defer server.Close()
	testCases := []struct {
		desc           string
		path           string
		method         string
		statusExpected int
	}{
		{
			desc:           "Test randomSlowRequest",
			path:           "/randomSlowRequest",
			method:         http.MethodGet,
			statusExpected: http.StatusOK,
		},
		{
			desc:           "Test slowRequest with wrong path",
			path:           "/slowRequest/abc",
			method:         http.MethodGet,
			statusExpected: http.StatusBadRequest,
		},
		{
			desc:           "Test slowRequest with right path",
			path:           "/slowRequest/1",
			method:         http.MethodGet,
			statusExpected: http.StatusOK,
		},
		{
			desc:           "Test healthCheck",
			path:           "/healthcheck",
			method:         http.MethodGet,
			statusExpected: http.StatusOK,
		},
		{
			desc:           "Test testMethods",
			path:           "/testMethods",
			method:         http.MethodPost,
			statusExpected: http.StatusOK,
		},
		{
			desc:           "Test echo",
			path:           "/",
			method:         http.MethodGet,
			statusExpected: http.StatusOK,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, fmt.Sprintf("%s%s", server.URL, tc.path), nil)
			if err != nil {
				t.Errorf("Got error %v with request %v", err, req)
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("Got error: %v", err)
			}
			if response.StatusCode != tc.statusExpected {
				t.Errorf("got statusCode %v, expect %v", response.StatusCode, tc.statusExpected)
			}
		})
	}
}

// Build a http router for test
func router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", healthCheck)
	mux.HandleFunc("/panic", triggerPanic)
	mux.HandleFunc("/randomSlowRequest", randomSlowRequest)
	mux.HandleFunc("/slowRequest/", slowRequest)
	mux.HandleFunc("/testMethod", testMethods)
	mux.HandleFunc("/", echo)
	return mux
}
