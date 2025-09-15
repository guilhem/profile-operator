/*
Copyright 2025.

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

package v1alpha1

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProfileBindingSpec defines the desired state of ProfileBinding
type ProfileBindingSpec struct {
	// profileRef references the Profile to apply
	// +required
	ProfileRef *ProfileReference `json:"profileRef,omitzero"`

	// targetSelector defines which resources this binding applies to
	// +required
	TargetSelector TargetSelector `json:"targetSelector,omitzero"`

	// updateStrategy defines how updates are rolled out to target resources
	// +kubebuilder:default={type:"Immediate"}
	// +optional
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`

	// enabled determines whether this binding is active
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// ProfileReference references a Profile resource
type ProfileReference struct {
	// name is the name of the Profile resource
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// TargetSelector defines criteria for selecting target resources
// Uses the same pattern as admission controllers for maximum power and familiarity
type TargetSelector struct {
	// resourceRule defines what resources to target
	// Same format as admissionregistration.k8s.io/v1.Rule
	// Allows targeting multiple API groups/versions/resources
	// +required
	ResourceRule admissionregistrationv1.Rule `json:"resourceRule,omitempty"`

	// namespaces where to look for resources
	// If empty, uses the ProfileBinding's namespace
	// +optional
	// +kubebuilder:validation:MaxItems=100
	// +listType=set
	Namespaces []string `json:"namespaces,omitempty"`

	// objectSelector selects specific objects within the targeted resources
	// +optional
	ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty"`
}

// UpdateStrategy defines how updates are applied to target resources
type UpdateStrategy struct {
	// type of update strategy
	// +kubebuilder:validation:Enum=Immediate
	// +kubebuilder:default="Immediate"
	// +required
	Type UpdateStrategyType `json:"type"`
}

// UpdateStrategyType defines the type of update strategy
type UpdateStrategyType string

const (
	// ImmediateUpdate applies changes to all matching resources immediately
	ImmediateUpdate UpdateStrategyType = "Immediate"
)

// ProfileBindingStatus defines the observed state of ProfileBinding.
type ProfileBindingStatus struct {
	// conditions represent the current state of the ProfileBinding resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// observedGeneration reflects the generation most recently observed by the controller
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// targetedResources is the count of resources that match the target selector
	// +optional
	TargetedResources *int32 `json:"targetedResources,omitempty"`

	// updatedResources is the count of resources that have been successfully updated
	// +optional
	UpdatedResources *int32 `json:"updatedResources,omitempty"`

	// failedResources is the count of resources that failed to be updated
	// +optional
	FailedResources *int32 `json:"failedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profileRef.name`
// +kubebuilder:printcolumn:name="Targeted",type=integer,JSONPath=`.status.targetedResources`
// +kubebuilder:printcolumn:name="Updated",type=integer,JSONPath=`.status.updatedResources`
// +kubebuilder:printcolumn:name="Failed",type=integer,JSONPath=`.status.failedResources`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProfileBinding is the Schema for the profilebindings API.
// A ProfileBinding applies a Profile to a set of target resources based on
// selection criteria and update strategy.
type ProfileBinding struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ProfileBinding
	// +required
	Spec ProfileBindingSpec `json:"spec,omitzero"`

	// status defines the observed state of ProfileBinding
	// +optional
	Status ProfileBindingStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ProfileBindingList contains a list of ProfileBinding
type ProfileBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProfileBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProfileBinding{}, &ProfileBindingList{})
}
