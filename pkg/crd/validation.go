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

	spec "github.com/go-openapi/spec"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/kube-openapi/pkg/common"
)

// validation returns a validation specification based on OpenAPI schema's.
func validation(typeSource string, fn common.GetOpenAPIDefinitions) (*apiextensionsv1beta1.CustomResourceValidation, error) {
	openapiSpec := fn(spec.MustCreateRef)
	// Condense schema for nested types into one master schema.
	condensedSchema := condenseSchema(openapiSpec[typeSource].Schema, openapiSpec)
	// Convert master schema into JSONSchemaProps by marshalling + unmarshalling.
	jsonSchemaProps := &apiextensionsv1beta1.JSONSchemaProps{}
	bytes, err := json.Marshal(condensedSchema)
	if err != nil {
		return nil, fmt.Errorf("error marshalling OpenAPI schema to JSON: %v", err)
	}
	err = json.Unmarshal(bytes, jsonSchemaProps)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling OpenAPI JSON: %v", err)
	}
	return &apiextensionsv1beta1.CustomResourceValidation{OpenAPIV3Schema: jsonSchemaProps}, nil
}

// condenseSchema replaces type references from a schema with the schema's for the referenced types.
func condenseSchema(currentSchema spec.Schema, openapiSpec map[string]common.OpenAPIDefinition) spec.Schema {
	currentSchemaProperties := currentSchema.SchemaProps.Properties
	for property, propertySchema := range currentSchemaProperties {
		ref := propertySchema.SchemaProps.Ref.String()
		if ref != "" {
			referencedSchema := openapiSpec[ref].Schema
			referencedSchema.SchemaProps.Type = spec.StringOrArray{"object"}
			propertySchema.SchemaProps = referencedSchema.SchemaProps
			currentSchemaProperties[property] = propertySchema
			condenseSchema(propertySchema, openapiSpec)
		}
	}
	// Apply fixes for certain known issues.
	currentSchema.AdditionalProperties = nil
	return currentSchema
}
