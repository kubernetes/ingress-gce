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
	"testing"

	"github.com/go-openapi/spec"
	"k8s.io/kube-openapi/pkg/common"
)

var (
	openapiSpec = map[string]common.OpenAPIDefinition{
		"Foo": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "Foo",
					Properties: map[string]spec.Schema{
						"bar": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.MustCreateRef("Bar"),
							},
						},
						"baz": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.MustCreateRef("Baz"),
							},
						},
						"quuxs": {
							SchemaProps: spec.SchemaProps{
								Type: []string{"array"},
								Items: &spec.SchemaOrArray{
									Schema: &spec.Schema{
										SchemaProps: spec.SchemaProps{
											Ref: spec.MustCreateRef("Quux"),
										},
									},
								},
							},
						},
						"ts": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.MustCreateRef("k8s.io/apimachinery/pkg/apis/meta/v1.Time"),
							},
						},
					},
				},
			},
		},
		"Bar": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "Bar",
					Properties: map[string]spec.Schema{
						"qux": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.MustCreateRef("Qux"),
							},
						},
					},
				},
			},
		},
		"Baz": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "Baz",
					Properties: map[string]spec.Schema{
						"prop": {
							SchemaProps: spec.SchemaProps{
								Type:   []string{"boolean"},
								Format: "",
							},
						},
					},
				},
			},
		},
		"Qux": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "Qux",
					Properties: map[string]spec.Schema{
						"prop": {
							SchemaProps: spec.SchemaProps{
								Type:   []string{"boolean"},
								Format: "",
							},
						},
					},
				},
			},
		},
		"Quux": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "Quux",
					Properties: map[string]spec.Schema{
						"qux": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.MustCreateRef("Qux"),
							},
						},
					},
				},
			},
		},
	}
)

func TestCondenseSchema(t *testing.T) {
	condensedFooSchema := condenseSchema(openapiSpec["Foo"].Schema, openapiSpec)

	// Verify that the condensed schema contains no references.
	if condensedFooSchema.SchemaProps.Properties["baz"].SchemaProps.Description != "Baz" {
		t.Errorf("Expected Foo's schema to contain the Description for Baz.")
	}
	if condensedFooSchema.SchemaProps.Properties["bar"].SchemaProps.Description != "Bar" {
		t.Errorf("Expected Foo's schema to contain the Description for Bar.")
	}
	condensedBarSchema := condensedFooSchema.SchemaProps.Properties["bar"]
	if condensedBarSchema.SchemaProps.Properties["qux"].SchemaProps.Description != "Qux" {
		t.Errorf("Expected Foo's schema for Bar to contain the Description for Qux.")
	}
	if condensedFooSchema.SchemaProps.Properties["quuxs"].SchemaProps.Items.Schema.SchemaProps.Description != "Quux" {
		t.Errorf("Expected Foo's schema to contain the Description for Quux.")
	}

	condensedQuuxSchema := condensedFooSchema.SchemaProps.Properties["quuxs"].SchemaProps.Items.Schema
	if condensedQuuxSchema.SchemaProps.Properties["qux"].Description != "Qux" {
		t.Errorf("Expected Foo's schema to contain the Description for Qux.")
	}

	tsProp := condensedFooSchema.SchemaProps.Properties["ts"]
	if !tsProp.SchemaProps.Type.Contains("string") {
		t.Errorf("Expected Foo's ts property to have type string")
	}
	if tsProp.SchemaProps.Format != "date-time" {
		t.Errorf("Expected Foo's ts property to have format 'date-time'")
	}
	if !tsProp.SchemaProps.Nullable {
		t.Errorf("Expected Foo's ts property to be Nullable")
	}
}
