/*
Copyright 2018 Google LLC

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

// Package ssl provides operations for manipulating SslCertificate GCE resources.
package ssl

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/golang/glog"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v0.alpha"
	"google.golang.org/api/googleapi"
	gcfg "gopkg.in/gcfg.v1"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

const httpTimeout = 30 * time.Second

type SSL struct {
	service   *compute.Service
	projectID string
}

func getTokenSource(cloudConfig string) (oauth2.TokenSource, error) {
	if cloudConfig != "" {
		glog.V(1).Info("In a GKE cluster")

		config, err := os.Open(cloudConfig)
		if err != nil {
			return nil, fmt.Errorf("Could not open cloud provider configuration %s: %v", cloudConfig, err)
		}
		defer config.Close()

		var cfg gce.ConfigFile
		if err := gcfg.ReadInto(&cfg, config); err != nil {
			return nil, fmt.Errorf("Could not read config %v", err)
		}

		return gce.NewAltTokenSource(cfg.Global.TokenURL, cfg.Global.TokenBody), nil
	} else if len(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")) > 0 {
		glog.V(1).Info("In a GCP cluster")
		return google.DefaultTokenSource(oauth2.NoContext, compute.ComputeScope)
	} else {
		glog.V(1).Info("Using default TokenSource")
		return google.ComputeTokenSource(""), nil
	}
}

func New(cloudConfig string) (*SSL, error) {
	tokenSource, err := getTokenSource(cloudConfig)
	if err != nil {
		return nil, err
	}

	glog.V(1).Infof("TokenSource: %v", tokenSource)

	projectID, err := metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("Could not fetch project id: %v", err)
	}

	client := oauth2.NewClient(oauth2.NoContext, tokenSource)
	client.Timeout = httpTimeout

	service, err := compute.New(client)
	if err != nil {
		return nil, err
	}

	return &SSL{
		service:   service,
		projectID: projectID,
	}, nil
}

// Create creates a new SslCertificate resource.
func (c *SSL) Create(sslCertificateName string, domains []string) error {
	sslCertificate := &compute.SslCertificate{
		Managed: &compute.SslCertificateManagedSslCertificate{
			Domains: domains,
		},
		Name: sslCertificateName,
		Type: "MANAGED",
	}

	_, err := c.service.SslCertificates.Insert(c.projectID, sslCertificate).Do()
	return err
}

// Delete deletes an SslCertificate resource.
func (c *SSL) Delete(name string) error {
	_, err := c.service.SslCertificates.Delete(c.projectID, name).Do()
	return err
}

// Exists returns false if an SslCertificate is deleted and true if it either exists or an error has occurred.
func (c *SSL) Exists(name string) (bool, error) {
	_, err := c.Get(name)
	if err == nil {
		return true, nil
	}

	gerr, ok := err.(*googleapi.Error)
	if ok && gerr.Code == http.StatusNotFound {
		return false, nil
	}

	return false, err
}

// Get fetches an SslCertificate resource.
func (c *SSL) Get(name string) (*compute.SslCertificate, error) {
	return c.service.SslCertificates.Get(c.projectID, name).Do()
}

// List fetches SslCertificate resources.
func (c *SSL) List() (*compute.SslCertificateList, error) {
	return c.service.SslCertificates.List(c.projectID).Do()
}
