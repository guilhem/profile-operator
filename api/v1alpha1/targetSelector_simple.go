package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Option 1: Pattern simple avec références directes (comme RoleBinding)
type SimpleTargetSelector struct {
	// targets is a list of specific resources to apply the profile to
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	// +listType=atomic
	Targets []ResourceTarget `json:"targets,omitempty"`
}

type ResourceTarget struct {
	// apiVersion of the target resource (e.g., "apps/v1", "v1")
	// +kubebuilder:validation:MinLength=1
	// +required
	APIVersion string `json:"apiVersion"`

	// kind of the target resource (e.g., "Deployment", "ConfigMap")
	// +kubebuilder:validation:MinLength=1
	// +required
	Kind string `json:"kind"`

	// name of the specific resource
	// If empty, applies to all resources of this kind in the namespace
	// +optional
	Name *string `json:"name,omitempty"`

	// namespace of the target resource
	// If empty, uses the ProfileBinding's namespace
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// labelSelector selects resources by labels (when name is empty)
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// Option 2: Pattern LabelSelector standard (comme Service)
type LabelBasedTargetSelector struct {
	// apiVersion of the target resources (e.g., "apps/v1")
	// +required
	APIVersion string `json:"apiVersion"`

	// kind of the target resources (e.g., "Deployment")
	// +required
	Kind string `json:"kind"`

	// namespace where to look for resources
	// If empty, uses the ProfileBinding's namespace
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// selector identifies which resources to target
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// Option 3: Pattern hybride simplifié (recommandé)
type StandardTargetSelector struct {
	// apiVersion of the target resources (e.g., "apps/v1")
	// +required
	APIVersion string `json:"apiVersion"`

	// kind of the target resources (e.g., "Deployment")
	// +required
	Kind string `json:"kind"`

	// namespace where to look for resources
	// Defaults to the ProfileBinding's namespace if empty
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Exactly one of the following must be specified:

	// Name selects a specific resource by name
	// +optional
	Name *string `json:"name,omitempty"`

	// labelSelector selects resources by labels
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}
