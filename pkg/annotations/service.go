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

package annotations

import (
	"encoding/json"
	"fmt"
)

const (
	// ProtocolHTTP protocol for a service
	ProtocolHTTP AppProtocol = "HTTP"
	// ProtocolHTTPS protocol for a service
	ProtocolHTTPS AppProtocol = "HTTPS"
)

// AppProtocol describes the service protocol.
type AppProtocol string

// Service represents Service annotations.
type Service map[string]string

// ApplicationProtocols returns a map of port (name or number) to the protocol
// on the port.
func (svc Service) ApplicationProtocols() (map[string]AppProtocol, error) {
	val, ok := svc[ServiceApplicationProtocolKey]
	if !ok {
		return map[string]AppProtocol{}, nil
	}

	var portToProtos map[string]AppProtocol
	err := json.Unmarshal([]byte(val), &portToProtos)

	// Verify protocol is an accepted value
	for _, proto := range portToProtos {
		switch proto {
		case ProtocolHTTP, ProtocolHTTPS:
		default:
			return nil, fmt.Errorf("invalid port application protocol: %v", proto)
		}
	}

	return portToProtos, err
}

// NEGEnabled is true if the service uses NEGs.
func (svc Service) NEGEnabled() bool {
	v, ok := svc[NetworkEndpointGroupAlphaAnnotation]
	return ok && v == "true"
}
