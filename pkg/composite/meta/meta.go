/*
Copyright 2019 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
https://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package meta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// This assumes that alpha contains a superset of all struct fields
	apiFilePath = "./vendor/google.golang.org/api/compute/v0.alpha/compute-api.json"
)

// MainServices describes all of the API types that we want to define all the helper functions for
// The other types that are discovered as dependencies will simply be wrapped with a composite struct
// The format of the map is ServiceName -> k8s-cloud-provider wrapper name
var MainServices = map[string]string{
	"Address":                         "Addresses",
	"BackendService":                  "BackendServices",
	"ForwardingRule":                  "ForwardingRules",
	"HealthCheck":                     "HealthChecks",
	"HealthStatusForNetworkEndpoint":  "HealthStatusForNetworkEndpoints",
	"UrlMap":                          "UrlMaps",
	"TargetHttpProxy":                 "TargetHttpProxies",
	"TargetHttpsProxy":                "TargetHttpsProxies",
	"SslCertificate":                  "SslCertificates",
	"NetworkEndpointGroup":            "NetworkEndpointGroups",
	"NetworkEndpoint":                 "NetworkEndpoints",
	"NetworkEndpointWithHealthStatus": "NetworkEndpointsWithHealthStatus",
	"NetworkEndpointGroupsAttachEndpointsRequest": "NetworkEndpointGroupsAttachEndpointsRequests",
	"NetworkEndpointGroupsDetachEndpointsRequest": "NetworkEndpointGroupsDetachEndpointsRequests",
	"NetworkEndpointGroupsListEndpointsRequest":   "NetworkEndpointGroupsListEndpointsRequests",
	"SignedUrlKey": "SignedUrlKey",
}

// TODO: (shance) Replace this with data gathered from meta.AllServices
// Services in NoUpdate will not have an Update() method generated for them
var NoUpdate = sets.NewString(
	"Address",
	"ForwardingRule",
	"HealthStatusForNetworkEndpoint",
	"TargetHttpProxy",
	"TargetHttpsProxy",
	"SslCertificate",
	"NetworkEndpointGroup",
	"NetworkEndpoint",
	"NetworkEndpointWithHealthStatus",
	"SignedUrlKey",
)

// Services in NoCRUD will not have Create, Get, Delete, Update, methods generated for them
var NoCRUD = sets.NewString(
	"HealthStatusForNetworkEndpoint",
	"NetworkEndpointGroupsAttachEndpointsRequest",
	"NetworkEndpointGroupsDetachEndpointsRequest",
	"NetworkEndpointGroupsListEndpointsRequest",
	"NetworkEndpoint",
	"NetworkEndpointWithHealthStatus",
	"SignedUrlKey",
)

// TODO: This should be a list, not a map so the generated code ordering is stable
// across generations.

var Versions = map[string]string{
	"Alpha": "alpha",
	"Beta":  "beta",
	"GA":    "",
}

// TODO (shance) Replace this with data gathered from meta.AllServices or discovery doc
// DefaultRegionalServices contains services which are regional by default.
// Their global type is explicitly labeled (e.g. GlobalForwardingRule)
var DefaultRegionalServices = sets.NewString(
	"Address",
	"ForwardingRule",
)

var DefaultZonalServices = sets.NewString(
	"NetworkEndpointGroup",
)

type GroupResourceInfo struct {
	AttachFuncName  string
	DetachFuncName  string
	ListFuncName    string
	AggListFuncName string
	AttachReqName   string
	DetachReqName   string
	ListReqName     string
	ListRespName    string
	AggListRespName string
}

// GroupResourceServices support adding/removing objects from them. Examples are Instance Groups, NetworkEndpointGroups.
// Additional APIs to Attach/Detach objects will be created for these services.
var GroupResourceServices = map[string]*GroupResourceInfo{
	"NetworkEndpointGroup": &GroupResourceInfo{
		AttachFuncName:  "AttachNetworkEndpoints",
		DetachFuncName:  "DetachNetworkEndpoints",
		AttachReqName:   "NetworkEndpointGroupsAttachEndpointsRequest",
		DetachReqName:   "NetworkEndpointGroupsDetachEndpointsRequest",
		ListFuncName:    "ListNetworkEndpoints",
		ListReqName:     "NetworkEndpointGroupsListEndpointsRequest",
		ListRespName:    "NetworkEndpointWithHealthStatus",
		AggListFuncName: "AggregatedList",
		AggListRespName: "NetworkEndpointGroup",
	},
}

// ApiService holds relevant data for generating a composite type + helper methods for a single API service
type ApiService struct {
	// Name of the Go struct
	Name string
	// Name used in the Json tag for marshalling/unmarshalling
	JsonName string
	// Force JSON tag as string type
	JsonStringOverride bool
	// Golang type
	GoType string
	// Name to use when creating an instance of this type
	VarName string
	// All of the struct fields
	Fields []ApiService
	// Comment describing the field
	Description string
}

// IsMainService() returns true if the service name is in the MainServices map
func (apiService *ApiService) IsMainService() bool {
	_, found := MainServices[apiService.Name]
	return found
}

// HasUpdate() returns true if the service name is *not* in the NoUpdate() list
func (apiService *ApiService) HasUpdate() bool {
	return !NoUpdate.Has(apiService.Name)
}

// HasCRUD() returns true if the service name is *not* in the NoCRUD() list
func (apiService *ApiService) HasCRUD() bool {
	return !NoCRUD.Has(apiService.Name)
}

func (apiService *ApiService) IsDefaultRegionalService() bool {
	return DefaultRegionalServices.Has(apiService.Name)
}

func (apiService *ApiService) IsDefaultZonalService() bool {
	return DefaultZonalServices.Has(apiService.Name)
}

func (apiService *ApiService) IsGroupResourceService() bool {
	_, ok := GroupResourceServices[apiService.Name]
	return ok
}

func (apiService *ApiService) GetGroupResourceInfo() *GroupResourceInfo {
	if !(apiService.IsGroupResourceService()) {
		return nil
	}
	return GroupResourceServices[apiService.Name]
}

// GetCloudProviderName() returns the name of the cloudprovider type for a service
func (apiService *ApiService) GetCloudProviderName() string {
	result, ok := MainServices[apiService.Name]
	if !ok {
		panic(fmt.Errorf("%s not present in map: %v", apiService.Name, MainServices))
	}

	return result
}

var AllApiServices []ApiService

// createVarName() converts the service name into camelcase
func createVarName(str string) string {
	copy := []rune(str)
	if len(copy) == 0 {
		return string(copy)
	}

	copy[0] = unicode.ToLower(rune(copy[0]))
	return string(copy)
}

// populateApiServices() parses the Api Spec and populates AllApiServices with the required services
// Performs BFS to resolve dependencies
func populateApiServices() {
	apiFile, err := os.Open(apiFilePath)
	if err != nil {
		panic(err)
	}
	defer apiFile.Close()

	byteValue, err := ioutil.ReadAll(apiFile)
	if err != nil {
		panic(err)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(byteValue), &result)

	// Queue of ApiService names for BFS
	typesQueue := []string{}
	// Set of already parsed ApiService names for BFS
	completed := sets.String{}
	// Go type of the property
	var propType string

	keys := []string{}
	for key := range MainServices {
		keys = append(keys, key)
	}
	typesQueue = append(typesQueue, keys...)

	for len(typesQueue) > 0 {
		typeName := typesQueue[0]
		typesQueue = typesQueue[1:]

		if completed.Has(typeName) {
			continue
		}
		completed.Insert(typeName)

		fields, ok := result["schemas"].(map[string]interface{})[typeName].(map[string]interface{})["properties"].(map[string]interface{})
		if !ok {
			panic(fmt.Errorf("Unable to parse type: %s", typeName))
		}

		apiService := ApiService{Name: typeName, Fields: []ApiService{}, VarName: createVarName(typeName)}

		for prop, val := range fields {
			subType := ApiService{Name: strings.Title(prop), JsonName: prop}

			var override bool
			propType, typesQueue, override, err = getGoType(val, typesQueue)
			if err != nil {
				panic(err)
			}
			subType.GoType = propType
			subType.JsonStringOverride = override
			desc, ok := val.(map[string]interface{})["description"]
			if !ok {
				fmt.Printf("WARNING: No description for type: %s.%s\n", typeName, prop)
			} else {
				subType.Description = asComment("\t", desc.(string))
			}
			apiService.Fields = append(apiService.Fields, subType)
		}

		// Sort fields since the keys aren't ordered deterministically
		sort.Slice(apiService.Fields[:], func(i, j int) bool {
			return apiService.Fields[i].Name < apiService.Fields[j].Name
		})

		AllApiServices = append(AllApiServices, apiService)
	}

	// Sort the struct definitions since the keys aren't ordered deterministically
	sort.Slice(AllApiServices[:], func(i, j int) bool {
		return AllApiServices[i].Name < AllApiServices[j].Name
	})
}

// getGoType() determines what the golang type is for a service by recursively descending the API spec json
// for a field.  Since this may discover new types, it also updates the typesQueue.
func getGoType(val interface{}, typesQueue []string) (string, []string, bool, error) {
	field, ok := val.(map[string]interface{})
	if !ok {
		panic(nil)
	}

	var err error
	var tmpType string
	var override bool

	propType := ""
	ref, ok := field["$ref"]
	// Field is not a built-in type, we need to wrap it
	if ok {
		refName := ref.(string)
		typesQueue = append(typesQueue, refName)
		propType = "*" + refName
	} else if field["type"] == "array" {
		tmpType, typesQueue, override, err = getGoType(field["items"], typesQueue)
		propType = "[]" + tmpType
	} else if field["type"] == "object" {
		addlProps, ok := field["additionalProperties"]
		if ok {
			tmpType, typesQueue, override, err = getGoType(addlProps, typesQueue)
			propType = "map[string]" + tmpType
		} else {
			propType = "map[string]string"
		}
	} else if format, ok := field["format"]; ok {
		if format.(string) == "byte" {
			propType = "string"
		} else if format.(string) == "float" {
			propType = "float64"
		} else if format.(string) == "int32" {
			propType = "int64"
		} else if format.(string) == "double" {
			propType = "float64"
		} else if format.(string) == "uint32" {
			propType = "int64"
		} else {
			propType = format.(string)
		}
	} else if field["type"] != "" {
		if field["type"].(string) == "boolean" {
			propType = "bool"
		} else {
			propType = field["type"].(string)
		}
	} else {
		err = fmt.Errorf("unable to get property type for prop: %v", val)
	}

	if field["type"] == "string" && propType != "string" {
		override = true
	}

	return propType, typesQueue, override, err
}

// Convert discovery description to properly formatted comment
func asComment(pfx, c string) string {
	var urlRE = regexp.MustCompile(`^http\S+$`)
	var buf bytes.Buffer
	const maxLen = 70
	r := strings.NewReplacer(
		"\n", "\n"+pfx+"// ",
		"`\"", `"`,
		"\"`", `"`,
	)
	for len(c) > 0 {
		line := c
		if len(line) < maxLen {
			fmt.Fprintf(&buf, "%s// %s\n", pfx, r.Replace(line))
			break
		}
		// Don't break URLs.
		if !urlRE.MatchString(line[:maxLen]) {
			line = line[:maxLen]
		}
		si := strings.LastIndex(line, " ")
		if nl := strings.Index(line, "\n"); nl != -1 && nl < si {
			si = nl
		}
		if si != -1 {
			line = line[:si]
		}
		fmt.Fprintf(&buf, "%s// %s\n", pfx, r.Replace(line))
		c = c[len(line):]
		if si != -1 {
			c = c[1:]
		}
	}
	return buf.String()
}

func init() {
	AllApiServices = []ApiService{}
	populateApiServices()
}
