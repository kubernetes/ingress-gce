package gce

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/go-ini/ini"
	cloudgce "k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/cmd/glbc/app"
	v1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/klog/v2"
)

func init() {
	// Disable pretty printing for INI files, to match default format of gce.conf.
	ini.PrettyFormat = false
	ini.PrettyEqual = true
	ini.PrettySection = true
}

// NewGCEForProviderConfig returns a new GCE client for the given project.
// If providerConfig is nil, it returns the default cloud associated with the cluster's project.
// It modifies the default configuration when a providerConfig is provided.
func NewGCEForProviderConfig(defaultConfigContent string, providerConfig *v1.ProviderConfig, logger klog.Logger) (*cloudgce.Cloud, error) {
	modifiedConfigContent, err := generateConfigForProviderConfig(defaultConfigContent, providerConfig)
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
	cfg, err := ini.Load([]byte(defaultConfigContent))
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
	tokenURL := globalSection.Key(tokenURLKey).String()
	projectNumberInt := providerConfig.Spec.ProjectNumber
	projectNumberStr := fmt.Sprintf("%d", projectNumberInt)
	newTokenURL := replaceProjectNumberInTokenURL(tokenURL, projectNumberStr)
	globalSection.Key(tokenURLKey).SetValue(newTokenURL)

	// Update TokenBody
	tokenBodyKey := "token-body"
	tokenBody := globalSection.Key(tokenBodyKey).String()
	newTokenBody, err := updateTokenProjectNumber(tokenBody, int(projectNumberInt))
	if err != nil {
		return "", fmt.Errorf("failed to update TokenBody: %v", err)
	}
	globalSection.Key(tokenBodyKey).SetValue(newTokenBody)

	// Update NetworkName and SubnetworkName
	if providerConfig.Spec.NetworkConfig != nil {
		networkNameKey := "network-name"
		globalSection.Key(networkNameKey).SetValue(providerConfig.Spec.NetworkConfig.Network)

		subnetworkNameKey := "subnetwork-name"
		globalSection.Key(subnetworkNameKey).SetValue(providerConfig.Spec.NetworkConfig.DefaultSubnetwork)
	}

	// Write the modified config content to a string with custom options
	var modifiedConfigContent bytes.Buffer
	_, err = cfg.WriteTo(&modifiedConfigContent)
	if err != nil {
		return "", fmt.Errorf("failed to write modified config content: %v", err)
	}

	return modifiedConfigContent.String(), nil
}

// replaceProjectNumberInTokenURL replaces the project number in the token URL.
func replaceProjectNumberInTokenURL(tokenURL string, projectNumber string) string {
	parts := strings.Split(tokenURL, "/")
	for i, part := range parts {
		if part == "projects" && i+1 < len(parts) {
			parts[i+1] = projectNumber
			break
		}
	}
	return strings.Join(parts, "/")
}

func updateTokenProjectNumber(tokenBody string, projectNumber int) (string, error) {
	var bodyMap map[string]interface{}

	// Unmarshal the JSON string into a map
	if err := json.Unmarshal([]byte(tokenBody), &bodyMap); err != nil {
		return "", fmt.Errorf("error unmarshaling TokenBody: %v", err)
	}

	// Update the "projectNumber" field with the new value
	bodyMap["projectNumber"] = projectNumber

	// Marshal the map back into a JSON string
	newTokenBodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return "", fmt.Errorf("error marshaling TokenBody: %v", err)
	}

	return string(newTokenBodyBytes), nil
}
