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

package crd

import (
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kube-openapi/pkg/common"
	spec "k8s.io/kube-openapi/pkg/validation/spec"
)

var metav1OpenAPISpec = map[string]common.OpenAPIDefinition{
	"k8s.io/apimachinery/pkg/apis/meta/v1.Time": {
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Type:     metav1.Time{}.OpenAPISchemaType(),
				Format:   metav1.Time{}.OpenAPISchemaFormat(),
				Nullable: true,
			},
		},
	},
	"k8s.io/api/core/v1.TypedLocalObjectReference": {
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Type: []string{"object"},
				Properties: map[string]spec.Schema{
					"apiGroup": {
						SchemaProps: spec.SchemaProps{
							Description: "APIGroup is the group for the resource being referenced. If APIGroup is not specified, the specified Kind must be in the core API group. For any other third-party types, APIGroup is required.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is the type of resource being referenced",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"name": {
						SchemaProps: spec.SchemaProps{
							Description: "Name is the name of resource being referenced",
							Type:        []string{"string"},
							Format:      "",
						},
					},
				},
				Required: []string{"kind", "name"},
			},
		},
	},
}

// validation returns a validation specification based on OpenAPI schema's.
func (v *Version) validation() (*apiextensionsv1.CustomResourceValidation, error) {
	if v.typeSource == "" || v.fn == nil {
		return nil, nil
	}
	openapiSpec := v.fn(spec.MustCreateRef)
	// Condense schema for nested types into one master schema.
	condensedSchema := condenseSchema(openapiSpec[v.typeSource].Schema, openapiSpec)
	// Convert master schema into JSONSchemaProps by marshalling + unmarshalling.
	jsonSchemaProps := &apiextensionsv1.JSONSchemaProps{}
	bytes, err := json.Marshal(condensedSchema)
	if err != nil {
		return nil, fmt.Errorf("error marshalling OpenAPI schema to JSON for %s API: %v", v.name, err)
	}
	err = json.Unmarshal(bytes, jsonSchemaProps)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling OpenAPI JSON for %s API: %v", v.name, err)
	}
	return &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: jsonSchemaProps}, nil
}

// condenseSchema replaces type references from a schema with the schema's for the referenced types.
func condenseSchema(currentSchema spec.Schema, openapiSpec map[string]common.OpenAPIDefinition) spec.Schema {
	currentSchemaProperties := currentSchema.SchemaProps.Properties
	for property, propertySchema := range currentSchemaProperties {

		if propertySchema.SchemaProps.Type.Contains("array") {
			ref := propertySchema.Items.Schema.SchemaProps.Ref.String()
			if ref != "" {
				referencedSchema := getReferenceSchema(ref, propertySchema, openapiSpec)
				condensedRefSchema := condenseSchema(referencedSchema, openapiSpec)
				propertySchema.SchemaProps.Items.Schema.SchemaProps = condensedRefSchema.SchemaProps
				currentSchemaProperties[property] = propertySchema
			}
			continue
		}

		ref := propertySchema.SchemaProps.Ref.String()
		if ref != "" {
			referencedSchema := getReferenceSchema(ref, propertySchema, openapiSpec)
			propertySchema.SchemaProps = referencedSchema.SchemaProps
			currentSchemaProperties[property] = propertySchema
			condenseSchema(propertySchema, openapiSpec)
		}
	}
	// Apply fixes for certain known issues.
	currentSchema.AdditionalProperties = nil
	return currentSchema
}

func getReferenceSchema(ref string, propertySchema spec.Schema, openapiSpec map[string]common.OpenAPIDefinition) spec.Schema {
	var referencedSchema spec.Schema
	// Check if reference exists in existing metav1 specs, otherwise look in provided openapi specs
	if refDef, ok := metav1OpenAPISpec[ref]; ok {
		referencedSchema = refDef.Schema
		referencedSchema.SchemaProps.Description = propertySchema.SchemaProps.Description
	} else {
		// If ref doesn't exist in either map, this will generate standard object one
		referencedSchema = openapiSpec[ref].Schema
		referencedSchema.SchemaProps.Type = spec.StringOrArray{"object"}
	}
	return referencedSchema
}
