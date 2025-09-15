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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

const (
	profileBindingFinalizer = "profilebinding.profiles.barpilot.io/finalizer"
	ConditionReady          = "Ready"
	ReasonApplied           = "Applied"
	ReasonFailed            = "Failed"
	ReasonInitializing      = "Initializing"
	ReasonDisabled          = "Disabled"
	ReasonProfileNotFound   = "ProfileNotFound"
	ReasonPartiallyApplied  = "PartiallyApplied"
)

// ProfileBindingReconciler reconciles a ProfileBinding object
type ProfileBindingReconciler struct {
	client.Client `json:",inline"`
	Scheme        *runtime.Scheme      `json:"-"`
	Recorder      record.EventRecorder `json:"-"`
	Applicator    *ProfileApplicator   `json:"-"`
}

// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profilebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profilebindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profilebindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profiles,verbs=get;list;watch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProfileBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ProfileBinding instance
	var profileBinding profilesv1alpha1.ProfileBinding
	if err := r.Get(ctx, req.NamespacedName, &profileBinding); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Always update status at the end
	defer r.updateStatus(ctx, &profileBinding)

	// Handle deletion
	if !profileBinding.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(&profileBinding, profileBindingFinalizer)
		return ctrl.Result{}, r.Update(ctx, &profileBinding)
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(&profileBinding, profileBindingFinalizer) {
		controllerutil.AddFinalizer(&profileBinding, profileBindingFinalizer)
		return ctrl.Result{RequeueAfter: time.Second}, r.Update(ctx, &profileBinding)
	}

	// Initialize conditions
	if len(profileBinding.Status.Conditions) == 0 {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonInitializing, "ProfileBinding is initializing")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Check if disabled
	if profileBinding.Spec.Enabled != nil && !*profileBinding.Spec.Enabled {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonDisabled, "ProfileBinding is disabled")
		return ctrl.Result{}, nil
	}

	// Get profile
	var profile profilesv1alpha1.Profile
	if err := r.Get(ctx, types.NamespacedName{Name: profileBinding.Spec.ProfileRef.Name}, &profile); err != nil {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonProfileNotFound, err.Error())
		return ctrl.Result{}, err
	}

	// Get target resources
	targetResources, err := r.getTargetResources(ctx, &profileBinding)
	if err != nil {
		log.Error(err, "Failed to get target resources")
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonFailed, err.Error())
		return ctrl.Result{}, err
	}

	log.Info("Found target resources", "count", len(targetResources))
	if len(targetResources) == 0 {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonInitializing, "No target resources found")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Apply profile to resources
	updated, failed, err := r.Applicator.ApplyProfileToResources(ctx, &profileBinding, &profile, targetResources)
	if err != nil {
		log.Error(err, "Failed to apply profile to resources")
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonFailed, err.Error())
		return ctrl.Result{}, err
	}

	// Update status
	profileBinding.Status.TargetedResources = ptr.To(int32(len(targetResources)))
	profileBinding.Status.UpdatedResources = ptr.To(int32(updated))
	profileBinding.Status.FailedResources = ptr.To(int32(failed))

	if failed == 0 {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionTrue, ReasonApplied, "Profile applied successfully")
		// Only requeue if there were changes to apply, otherwise stay ready
		return ctrl.Result{}, nil
	} else {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonPartiallyApplied, fmt.Sprintf("%d resources failed", failed))
		// Requeue on failure to retry
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}
}

// getTargetResources retrieves resources based on the target selector
func (r *ProfileBindingReconciler) getTargetResources(ctx context.Context, binding *profilesv1alpha1.ProfileBinding) ([]unstructured.Unstructured, error) {
	targetSelector := &binding.Spec.TargetSelector
	resourceRule := &targetSelector.ResourceRule

	// Determine target namespaces
	targetNamespaces := targetSelector.Namespaces
	if len(targetNamespaces) == 0 {
		targetNamespaces = []string{binding.Namespace} // Default to ProfileBinding's namespace
	}

	var allResources []unstructured.Unstructured

	// Iterate through all API groups, versions, and resources in the rule
	for _, apiGroup := range resourceRule.APIGroups {
		for _, apiVersion := range resourceRule.APIVersions {
			for _, resource := range resourceRule.Resources {
				// Parse the GVK
				gvk := schema.GroupVersionKind{
					Group:   apiGroup,
					Version: apiVersion,
					Kind:    r.resourceToKind(resource), // Convert resource to Kind
				}

				// Get resources in all target namespaces
				for _, namespace := range targetNamespaces {
					resources, err := r.getResourcesInNamespace(ctx, gvk, namespace, targetSelector)
					if err != nil {
						return nil, fmt.Errorf("failed to get resources in namespace %s: %w", namespace, err)
					}
					allResources = append(allResources, resources...)
				}
			}
		}
	}

	return allResources, nil
}

// resourceToKind converts a resource name to its Kind (simple heuristic)
func (r *ProfileBindingReconciler) resourceToKind(resource string) string {
	// Simple conversion: deployments -> Deployment, pods -> Pod, etc.
	switch resource {
	case "deployments":
		return "Deployment"
	case "statefulsets":
		return "StatefulSet"
	case "daemonsets":
		return "DaemonSet"
	case "replicasets":
		return "ReplicaSet"
	case "pods":
		return "Pod"
	case "services":
		return "Service"
	case "configmaps":
		return "ConfigMap"
	case "secrets":
		return "Secret"
	default:
		// Capitalize first letter as fallback
		if len(resource) > 0 {
			return strings.ToUpper(resource[:1]) + resource[1:]
		}
		return resource
	}
}

// getResourcesInNamespace gets resources in a specific namespace
func (r *ProfileBindingReconciler) getResourcesInNamespace(ctx context.Context, gvk schema.GroupVersionKind, namespace string, selector *profilesv1alpha1.TargetSelector) ([]unstructured.Unstructured, error) {
	resourceList := &unstructured.UnstructuredList{}
	resourceList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	listOpts := []client.ListOption{client.InNamespace(namespace)}

	// Add object selector if provided
	if selector.ObjectSelector != nil {
		selectorObj, err := metav1.LabelSelectorAsSelector(selector.ObjectSelector)
		if err != nil {
			return nil, err
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selectorObj})
	}

	if err := r.List(ctx, resourceList, listOpts...); err != nil {
		return nil, err
	}

	return resourceList.Items, nil
}

// setCondition sets a condition with current timestamp
func (r *ProfileBindingReconciler) setCondition(binding *profilesv1alpha1.ProfileBinding, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, condition)
}

// updateStatus updates the ProfileBinding status
func (r *ProfileBindingReconciler) updateStatus(ctx context.Context, binding *profilesv1alpha1.ProfileBinding) {
	binding.Status.ObservedGeneration = &binding.Generation
	if err := r.Status().Update(ctx, binding); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to update status")
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *ProfileBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&profilesv1alpha1.ProfileBinding{}).
		Complete(r)
}
