package gce

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-ini/ini"
	cloudgce "k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/cmd/glbc/app"
	v1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/klog/v2"
)

func init() {
	// Disable pretty printing for INI files, to match default format of gce.conf.
	ini.PrettyFormat = false
	ini.PrettyEqual = true
	ini.PrettySection = true
}

type GCECreator interface {
	GCEForProviderConfig(providerConfig *v1.ProviderConfig, logger klog.Logger) (*cloudgce.Cloud, error)
}

type DefaultGCECreator struct {
	defaultConfigFileString string
}

func NewDefaultGCECreator(logger klog.Logger) (*DefaultGCECreator, error) {
	defaultGCEConfig, err := app.GCEConfString(logger)
	if err != nil {
		return nil, fmt.Errorf("error getting default cluster GCE config: %v", err)
	}
	return &DefaultGCECreator{
		defaultConfigFileString: defaultGCEConfig,
	}, nil
}

// GCEForProviderConfig returns a new GCE client for the given project.
// If providerConfig is nil, it returns the default cloud associated with the cluster's project.
// It modifies the default configuration when a providerConfig is provided.
func (g *DefaultGCECreator) GCEForProviderConfig(providerConfig *v1.ProviderConfig, logger klog.Logger) (*cloudgce.Cloud, error) {
	modifiedConfigContent, err := generateConfigForProviderConfig(g.defaultConfigFileString, providerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to modify config content: %v", err)
	}

	// Return a new GCE client using the modified configuration content
	return app.GCEClientForConfigReader(
		func() io.Reader { return strings.NewReader(modifiedConfigContent) },
		logger,
	), nil
}

func generateConfigForProviderConfig(defaultConfigContent string, providerConfig *v1.ProviderConfig) (string, error) {
	if providerConfig == nil {
		return defaultConfigContent, nil
	}

	// Load the config content into an INI file
	cfg, err := ini.LoadSources(ini.LoadOptions{
		AllowShadows: true, // This allows multiple keys with the same name
	}, []byte(defaultConfigContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse default config content: %w", err)
	}

	globalSection := cfg.Section("global")
	if globalSection == nil {
		return "", fmt.Errorf("global section not found in config")
	}

	// Update ProjectID
	projectIDKey := "project-id"
	globalSection.Key(projectIDKey).SetValue(providerConfig.Spec.ProjectID)

	// Update TokenURL
	tokenURLKey := "token-url"
	oldValue := globalSection.Key(tokenURLKey).String()
	tokenURL, err := tokenURLForProviderConfig(oldValue, providerConfig)
	if err != nil {
		return "", fmt.Errorf("failed to update TokenURL: %v", err)
	}
	globalSection.Key(tokenURLKey).SetValue(tokenURL)

	// Update TokenBody
	tokenBodyKey := "token-body"
	tokenBody := globalSection.Key(tokenBodyKey).String()
	newTokenBody, err := updateTokenProjectNumber(tokenBody, int(providerConfig.Spec.ProjectNumber))
	if err != nil {
		return "", fmt.Errorf("failed to update TokenBody: %v", err)
	}
	globalSection.Key(tokenBodyKey).SetValue(newTokenBody)

	// Update NetworkName and SubnetworkName
	networkNameKey := "network-name"
	// Network name is the last part of the network path
	// e.g. projects/my-project/global/networks/my-network -> my-network
	networkParts := strings.Split(providerConfig.Spec.NetworkConfig.Network, "/")
	networkName := providerConfig.Spec.NetworkConfig.Network
	if len(networkParts) > 1 {
		networkName = networkParts[len(networkParts)-1]
	}
	globalSection.Key(networkNameKey).SetValue(networkName)

	subnetworkNameKey := "subnetwork-name"
	// Subnetwork name is the last part of the subnetwork path
	// e.g. projects/my-project/regions/us-central1/subnetworks/my-subnetwork -> my-subnetwork
	subnetworkParts := strings.Split(providerConfig.Spec.NetworkConfig.SubnetInfo.Subnetwork, "/")
	subnetworkName := providerConfig.Spec.NetworkConfig.SubnetInfo.Subnetwork
	if len(subnetworkParts) > 1 {
		subnetworkName = subnetworkParts[len(subnetworkParts)-1]
	}
	globalSection.Key(subnetworkNameKey).SetValue(subnetworkName)

	// Write the modified config content to a string with custom options
	var modifiedConfigContent bytes.Buffer
	_, err = cfg.WriteTo(&modifiedConfigContent)
	if err != nil {
		return "", fmt.Errorf("failed to write modified config content: %v", err)
	}

	return modifiedConfigContent.String(), nil
}

func tokenURLForProviderConfig(existingTokenURL string, providerConfig *v1.ProviderConfig) (string, error) {
	if _, ok := providerConfig.Labels[flags.F.MultiProjectOwnerLabelKey]; !ok {
		// If no owner label is set, assume token URL belongs to the default project.
		return existingTokenURL, nil
	}

	// Extract location from the old token URL
	location := extractLocationFromTokenURL(existingTokenURL)
	// Extract the baseURL before "/projects/"
	tokenURLParts := strings.SplitN(existingTokenURL, "/projects/", 2)
	if len(tokenURLParts) != 2 {
		return "", fmt.Errorf("invalid token URL format")
	}
	baseURL := tokenURLParts[0]
	// Format: {BASE_URL}/projects/{TENANT_PROJECT_NUMBER}/locations/{TENANT_LOCATION}/tenants/{TENANT_ID}:generateTenantToken"
	formatString := "%s/projects/%d/locations/%s/tenants/%s:generateTenantToken"
	tokenURL := fmt.Sprintf(formatString, baseURL, providerConfig.Spec.ProjectNumber, location, providerConfig.Name)
	return tokenURL, nil
}

func updateTokenProjectNumber(tokenBody string, projectNumber int) (string, error) {
	// Check if the token body is a quoted JSON string
	isQuoted := len(tokenBody) > 0 && tokenBody[0] == '"' && tokenBody[len(tokenBody)-1] == '"'

	var jsonStr string
	if isQuoted {
		// Unquote the JSON string
		var err error
		jsonStr, err = strconv.Unquote(tokenBody)
		if err != nil {
			return "", fmt.Errorf("error unquoting TokenBody: %v", err)
		}
	} else {
		jsonStr = tokenBody
	}

	var bodyMap map[string]any

	// Unmarshal the JSON string into a map
	if err := json.Unmarshal([]byte(jsonStr), &bodyMap); err != nil {
		return "", fmt.Errorf("error unmarshaling TokenBody: %v", err)
	}

	// Update the "projectNumber" field with the new value
	bodyMap["projectNumber"] = projectNumber

	// Marshal the map back into a JSON string
	newTokenBodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return "", fmt.Errorf("error marshaling TokenBody: %v", err)
	}

	if isQuoted {
		// Re-quote the JSON string if the original was quoted
		return strconv.Quote(string(newTokenBodyBytes)), nil
	}

	return string(newTokenBodyBytes), nil
}

// extractLocationFromTokenURL extracts the location from a GKE token URL.
// Example input: https://gkeauth.googleapis.com/v1/projects/654321/locations/us-central1/clusters/example-cluster:generateToken
// Returns: us-central1
func extractLocationFromTokenURL(tokenURL string) string {
	parts := strings.Split(tokenURL, "/")
	for i, part := range parts {
		if part == "locations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
