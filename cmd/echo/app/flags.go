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

package app

import (
	"flag"
	"time"
)

const (
	DefaultCertLifeSpan = 24 * time.Hour * 365
)

var (
	F = struct {
		CertificateLifeSpan time.Duration
		HTTPPort            int
		HTTPSPort           int
	}{}
)

// RegisterFlags creates flags.
func RegisterFlags() {
	flag.DurationVar(&F.CertificateLifeSpan, "cert-duration", DefaultCertLifeSpan, "The lifespan of the TLS certificate created on binary start")
	flag.IntVar(&F.HTTPPort, "http-port", 8080, "Port use for HTTP, 0 will disable this protocol")
	flag.IntVar(&F.HTTPSPort, "https-port", 8443, "Port use for HTTPS, 0 will disable this protocol")
}
