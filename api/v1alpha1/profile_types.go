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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProfileSpec defines the desired state of Profile
type ProfileSpec struct {
	// description provides a human-readable description of what this profile does
	// +kubebuilder:validation:MaxLength=512
	// +optional
	Description *string `json:"description,omitempty"`

	// template defines the configuration template to apply to target resources
	// This contains the JSON/YAML patches to be applied
	// +required
	Template *ProfileTemplate `json:"template"`

	// priority defines the priority of this profile when multiple profiles match the same resource
	// Higher values take precedence. Default is 0.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Priority *int32 `json:"priority,omitempty"`
}

// ProfileTemplate defines the template configuration to apply as a complete overlay
type ProfileTemplate struct {
	// patchStrategicMerge contains the complete YAML/JSON overlay to apply to target resources
	// This works like Kustomize overlays - any field specified here will be merged
	// into the target resource. Works with spec, metadata, data, and any other fields.
	// +optional
	PatchStrategicMerge *runtime.RawExtension `json:"patchStrategicMerge,omitempty"`
}

// ProfileStatus defines the observed state of Profile.
type ProfileStatus struct {
	// conditions represent the current state of the Profile resource.
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

	// lastUpdated is the last time this status was updated
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Priority",type=integer,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Profile is the Schema for the profiles API.
// A Profile defines a template configuration that can be applied to Kubernetes resources
// through ProfileBindings. Profiles are cluster-scoped resources that contain reusable
// configuration templates.
type Profile struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Profile
	// +required
	Spec *ProfileSpec `json:"spec,omitempty"`

	// status defines the observed state of Profile
	// +optional
	Status ProfileStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ProfileList contains a list of Profile
type ProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Profile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Profile{}, &ProfileList{})
}
