package gce

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
)

func TestReplaceProjectNumberInTokenURL(t *testing.T) {
	testCases := []struct {
		name           string
		tokenURL       string
		projectNumber  string
		expectedResult string
	}{
		{
			name:           "Replace project number in URL",
			tokenURL:       "https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken",
			projectNumber:  "654321",
			expectedResult: "https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/clusters/example-cluster:generateToken",
		},
		{
			name:           "URL without 'projects' segment",
			tokenURL:       "https://api.example.com/v1/locations/us-central1/clusters/example-cluster:generateToken",
			projectNumber:  "654321",
			expectedResult: "https://api.example.com/v1/locations/us-central1/clusters/example-cluster:generateToken",
		},
		{
			name:           "Empty token URL",
			tokenURL:       "",
			projectNumber:  "654321",
			expectedResult: "",
		},
		{
			name:           "URL with multiple 'projects' segments",
			tokenURL:       "https://api.example.com/v1/projects/12345/resources/projects/67890/details",
			projectNumber:  "654321",
			expectedResult: "https://api.example.com/v1/projects/654321/resources/projects/67890/details",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := replaceProjectNumberInTokenURL(tc.tokenURL, tc.projectNumber)
			if result != tc.expectedResult {
				t.Errorf("replaceProjectNumberInTokenURL(%q, %q) = %q; want %q", tc.tokenURL, tc.projectNumber, result, tc.expectedResult)
			}
		})
	}
}

func TestUpdateTokenBodyField(t *testing.T) {
	testCases := []struct {
		name           string
		tokenBody      string
		value          int
		expectedResult string
		expectError    bool
	}{
		{
			name:           "Update existing field",
			tokenBody:      `{"projectNumber":12345,"clusterId":"example-cluster"}`,
			value:          654321,
			expectedResult: `{"clusterId":"example-cluster","projectNumber":654321}`,
			expectError:    false,
		},
		{
			name:           "Add new field",
			tokenBody:      `{"clusterId":"example-cluster"}`,
			value:          654321,
			expectedResult: `{"clusterId":"example-cluster","projectNumber":654321}`,
			expectError:    false,
		},
		{
			name:           "Invalid JSON",
			tokenBody:      `{"clusterId":"example-cluster"`,
			value:          654321,
			expectedResult: "",
			expectError:    true,
		},
		{
			name:           "Empty token body",
			tokenBody:      "",
			value:          654321,
			expectedResult: "",
			expectError:    true,
		},
		{
			name:           "Nested JSON structure",
			tokenBody:      `{"data": {"projectNumber": 12345},"clusterId":"example-cluster"}`,
			value:          654321,
			expectedResult: `{"clusterId":"example-cluster","data":{"projectNumber":12345},"projectNumber":654321}`,
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := updateTokenProjectNumber(tc.tokenBody, tc.value)
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error, got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if !tc.expectError {
				// Normalize JSON strings for comparison
				expectedJSON := mustNormalizeJSON(t, tc.expectedResult)
				resultJSON := mustNormalizeJSON(t, result)

				if expectedJSON != resultJSON {
					t.Errorf("Expected result: %s, got: %s", expectedJSON, resultJSON)
				}
			}
		})
	}
}

func TestGenerateConfigForProviderConfig(t *testing.T) {
	testCases := []struct {
		name                 string
		defaultConfigContent string
		providerConfig       *v1.ProviderConfig
		expectedConfig       string
		expectError          bool
	}{
		{
			name: "Nil providerConfig returns default config content",
			defaultConfigContent: `
[global]
project-id = default-project-id
token-url = default-token-url
token-body = default-token-body
network-name = default-network
subnetwork-name = default-subnetwork
`,
			providerConfig: nil,
			expectedConfig: `[global]
project-id = default-project-id
token-url = default-token-url
token-body = default-token-body
network-name = default-network
subnetwork-name = default-subnetwork
`,
			expectError: false,
		},
		{
			name: "Valid providerConfig modifies config",
			defaultConfigContent: `
[global]
project-id = default-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"projectNumber":12345,"clusterId":"example-cluster"}
network-name = default-network
subnetwork-name = default-subnetwork
`,
			providerConfig: &v1.ProviderConfig{
				Spec: v1.ProviderConfigSpec{
					ProjectID:     "providerconfig-project-id",
					ProjectNumber: 654321,
					NetworkConfig: v1.ProviderNetworkConfig{
						Network: "projects/providerconfig-project-id/global/networks/providerconfig-network-url",
						SubnetInfo: v1.ProviderConfigSubnetInfo{
							Subnetwork: "projects/providerconfig-project-id/regions/us-central1/subnetworks/providerconfig-subnetwork-url",
						},
					},
				},
			},
			expectedConfig: `[global]
project-id = providerconfig-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"clusterId":"example-cluster","projectNumber":654321}
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
`,
			expectError: false,
		},
		{
			// This is a bit of defensive coding, just in case the network and subnetwork names are used instead of URLs.
			name: "ProviderConfig with network and subnetwork names, instead of URLs",
			defaultConfigContent: `
[global]
project-id = default-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"projectNumber":12345,"clusterId":"example-cluster"}
network-name = providerconfig-network-name
subnetwork-name = providerconfig-subnetwork-name
`,
			providerConfig: &v1.ProviderConfig{
				Spec: v1.ProviderConfigSpec{
					ProjectID:     "providerconfig-project-id",
					ProjectNumber: 654321,
					NetworkConfig: v1.ProviderNetworkConfig{
						Network: "providerconfig-network-name",
						SubnetInfo: v1.ProviderConfigSubnetInfo{
							Subnetwork: "providerconfig-subnetwork-name",
						},
					},
				},
			},
			expectedConfig: `[global]
project-id = providerconfig-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"clusterId":"example-cluster","projectNumber":654321}
network-name = providerconfig-network-name
subnetwork-name = providerconfig-subnetwork-name
`,
			expectError: false,
		},
		{
			name: "Valid providerConfig modifies config, does not modify other fields",
			defaultConfigContent: `
[global]
project-id = default-project-id
some-other-field = some-other-value
token-url = https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"projectNumber":12345,"clusterId":"example-cluster"}
network-name = default-network
subnetwork-name = default-subnetwork
other-field = other-value
`,
			providerConfig: &v1.ProviderConfig{
				Spec: v1.ProviderConfigSpec{
					ProjectID:     "providerconfig-project-id",
					ProjectNumber: 654321,
					NetworkConfig: v1.ProviderNetworkConfig{
						Network: "providerconfig-network-url",
						SubnetInfo: v1.ProviderConfigSubnetInfo{
							Subnetwork: "providerconfig-subnetwork-url",
						},
					},
				},
			},
			expectedConfig: `[global]
project-id = providerconfig-project-id
some-other-field = some-other-value
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"clusterId":"example-cluster","projectNumber":654321}
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
other-field = other-value
`,
			expectError: false,
		},
		{
			name: "Error updating TokenBody due to invalid JSON",
			defaultConfigContent: `
[global]
token-body = {"projectNumber":12345,"clusterId":"example-cluster"
`,
			providerConfig: &v1.ProviderConfig{
				Spec: v1.ProviderConfigSpec{
					ProjectID:     "providerconfig-project-id",
					ProjectNumber: 654321,
				},
			},
			expectedConfig: "",
			expectError:    true,
		},
		{
			name: "Missing global section",
			defaultConfigContent: `
[other]
key = value
`,
			providerConfig: &v1.ProviderConfig{
				Spec: v1.ProviderConfigSpec{
					ProjectID:     "providerconfig-project-id",
					ProjectNumber: 654321,
				},
			},
			expectedConfig: "",
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			modifiedConfig, err := generateConfigForProviderConfig(tc.defaultConfigContent, tc.providerConfig)
			if tc.expectError != (err != nil) {
				t.Errorf("Expected error: %v, got: %v", tc.expectError, err)
			}
			if !tc.expectError {
				// Remove whitespace differences for comparison
				expectedConfig := strings.TrimSpace(tc.expectedConfig)
				modifiedConfig = strings.TrimSpace(modifiedConfig)

				if diff := cmp.Diff(expectedConfig, modifiedConfig); diff != "" {
					t.Errorf("Modified config mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func mustNormalizeJSON(t *testing.T, jsonString string) string {
	t.Helper()
	var temp interface{}
	if err := json.Unmarshal([]byte(jsonString), &temp); err != nil {
		t.Fatalf("Invalid JSON string: %v", err)
	}
	normalizedBytes, err := json.Marshal(temp)
	if err != nil {
		t.Fatalf("Error marshaling JSON: %v", err)
	}
	return string(normalizedBytes)
}
