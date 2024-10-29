package projectcrd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/flags"
)

// Project is the Schema for the Project resource in the Multi-Project cluster.
type Project struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   ProjectSpec
	Status ProjectStatus
}

// ProjectName returns the name of the project which this CRD represents.
func (p *Project) ProjectName() string {
	return p.ObjectMeta.Labels[flags.F.MultiProjectCRDProjectNameLabel]
}

// ProjectList contains a list of Projects.
type ProjectList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []Project
}

// ProjectSpec specifies the desired state of the project in the MT cluster.
type ProjectSpec struct {
	// GCP project number where the project is to be created.
	ProjectNumber int64
	// Network configuration for the project.
	NetworkConfig NetworkConfig
}

// NetworkConfig specifies the network configuration for the project.
type NetworkConfig struct {
	Network           string
	DefaultSubnetwork string
}

// ProjectStatus stores the observed state of the project.
type ProjectStatus struct {
	// Conditions describe the current state of the Project.
	Conditions []metav1.Condition
}
