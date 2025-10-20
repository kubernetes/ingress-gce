package gce

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/flags"
)

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
		{
			name:           "Quoted token-body",
			tokenBody:      `"{\"projectNumber\":12345,\"clusterId\":\"example-cluster\"}"`,
			value:          654321,
			expectedResult: `"{\"projectNumber\":654321,\"clusterId\":\"example-cluster\"}"`,
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
token-body = "{\"projectNumber\":12345,\"clusterId\":\"example-cluster\"}"
network-name = default-network
subnetwork-name = default-subnetwork
`,
			providerConfig: nil,
			expectedConfig: `[global]
project-id = default-project-id
token-url = default-token-url
token-body = "{\"projectNumber\":12345,\"clusterId\":\"example-cluster\"}"
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
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
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
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/tenants/example-provider-config:generateTenantToken
token-body = {"clusterId":"example-cluster","projectNumber":654321}
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
`,
			expectError: false,
		},
		{
			name: "Valid providerConfig without owner label does not modify token-url",
			defaultConfigContent: `
[global]
project-id = default-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"projectNumber":12345,"clusterId":"example-cluster"}
network-name = default-network
subnetwork-name = default-subnetwork
`,
			providerConfig: &v1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
				},
				Spec: v1.ProviderConfigSpec{
					ProjectID:     "default-project-id",
					ProjectNumber: 12345,
					NetworkConfig: v1.ProviderNetworkConfig{
						Network: "projects/default-project-id/global/networks/providerconfig-network-url",
						SubnetInfo: v1.ProviderConfigSubnetInfo{
							Subnetwork: "projects/default-project-id/regions/us-central1/subnetworks/providerconfig-subnetwork-url",
						},
					},
				},
			},
			expectedConfig: `[global]
project-id = default-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"clusterId":"example-cluster","projectNumber":12345}
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
`,
			expectError: false,
		},
		{
			name: "Non standard base URL in token-url, providerConfig modifies config",
			defaultConfigContent: `
[global]
project-id = default-project-id
token-url = https://staging-gkeauth.googleapis.com/v60/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"projectNumber":12345,"clusterId":"example-cluster"}
network-name = default-network
subnetwork-name = default-subnetwork
`,
			providerConfig: &v1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
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
token-url = https://staging-gkeauth.googleapis.com/v60/projects/654321/locations/us-central1/tenants/example-provider-config:generateTenantToken
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
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
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
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/tenants/example-provider-config:generateTenantToken
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
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
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
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/tenants/example-provider-config:generateTenantToken
token-body = {"clusterId":"example-cluster","projectNumber":654321}
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
other-field = other-value
`,
			expectError: false,
		},
		{
			name: "Quoted token-body",
			defaultConfigContent: `
[global]
token-url = https://customgkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = "{\"projectNumber\":12345,\"clusterId\":\"example-cluster\"}"
project-id = default-project-id
stack-type = IPV4_IPV6
network-name = default-network
subnetwork-name = default-subnetwork
node-instance-prefix = example-cluster
node-tags = example-cluster-0e4cae0a-node
multizone = true
regional = true
alpha-features = ILBSubsets
`,
			providerConfig: &v1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
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
token-url = https://customgkeauth.googleapis.com/v1/projects/654321/locations/us-central1/tenants/example-provider-config:generateTenantToken
token-body = "{\"projectNumber\":654321,\"clusterId\":\"example-cluster\"}"
project-id = providerconfig-project-id
stack-type = IPV4_IPV6
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
node-instance-prefix = example-cluster
node-tags = example-cluster-0e4cae0a-node
multizone = true
regional = true
alpha-features = ILBSubsets
`,
			expectError: false,
		},

		{
			name: "Multiple keys (alpha-features) with the same name",
			defaultConfigContent: `
[global]
project-id = default-project-id
token-url = https://gkeauth.googleapis.com/v1/projects/12345/locations/us-central1/clusters/example-cluster:generateToken
token-body = {"projectNumber":12345,"clusterId":"example-cluster"}
network-name = default-network
subnetwork-name = default-subnetwork
alpha-features = ILBSubsets
alpha-features = ILBCustomSubnet
`,
			providerConfig: &v1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-provider-config",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
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
token-url = https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/tenants/example-provider-config:generateTenantToken
token-body = {"clusterId":"example-cluster","projectNumber":654321}
network-name = providerconfig-network-url
subnetwork-name = providerconfig-subnetwork-url
alpha-features = ILBSubsets
alpha-features = ILBCustomSubnet
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
				// Compare configs by normalizing each line
				expectedLines := normalizeConfig(t, tc.expectedConfig)
				actualLines := normalizeConfig(t, modifiedConfig)

				if diff := cmp.Diff(expectedLines, actualLines); diff != "" {
					t.Errorf("Modified config mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// normalizeConfig splits config into lines and normalizes the token-body JSON if present
func normalizeConfig(t *testing.T, config string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(config), "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "token-body =") {
			// Handle token-body line - normalize the JSON part
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				tokenBody := strings.TrimSpace(parts[1])

				// Use mustNormalizeJSON which now handles quoted strings
				normalized := mustNormalizeJSON(t, tokenBody)

				line = "token-body = " + normalized
			}
		}
		result = append(result, line)
	}

	return result
}

func mustNormalizeJSON(t *testing.T, jsonString string) string {
	t.Helper()

	// Check if the string is a quoted JSON string (starts and ends with quotes)
	isQuoted := false
	if len(jsonString) >= 2 && jsonString[0] == '"' && jsonString[len(jsonString)-1] == '"' {
		isQuoted = true
		// Remove outer quotes and unescape internal quotes
		jsonString = jsonString[1 : len(jsonString)-1]
		jsonString = strings.ReplaceAll(jsonString, "\\\"", "\"")
	}

	var temp any
	if err := json.Unmarshal([]byte(jsonString), &temp); err != nil {
		t.Fatalf("Invalid JSON string: %v", err)
	}

	normalizedBytes, err := json.Marshal(temp)
	if err != nil {
		t.Fatalf("Error marshaling JSON: %v", err)
	}

	normalizedString := string(normalizedBytes)

	// If the original was quoted, requote and escape the normalized result
	if isQuoted {
		normalizedString = "\"" + strings.ReplaceAll(normalizedString, "\"", "\\\"") + "\""
	}

	return normalizedString
}
