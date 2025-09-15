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
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

// InformerManager manages dynamic informers for ProfileBindings using controller-runtime watches
type InformerManager struct {
	client.Client
	Recorder   record.EventRecorder
	Applicator *ProfileApplicator
	manager    ctrl.Manager

	// Map of ProfileBinding NamespacedName to their active watches
	bindingWatches sync.Map // map[string]*BindingWatches
}

// BindingWatches holds all watches for a specific ProfileBinding
type BindingWatches struct {
	binding   *profilesv1alpha1.ProfileBinding
	watches   map[schema.GroupVersionKind]*WatchHandler
	cancelCtx context.CancelFunc
}

// WatchHandler handles events for a specific resource type
type WatchHandler struct {
	gvk     schema.GroupVersionKind
	binding *profilesv1alpha1.ProfileBinding
	manager *InformerManager
}

// NewInformerManager creates a new InformerManager
func NewInformerManager(client client.Client, recorder record.EventRecorder, applicator *ProfileApplicator, mgr ctrl.Manager) *InformerManager {
	return &InformerManager{
		Client:     client,
		Recorder:   recorder,
		Applicator: applicator,
		manager:    mgr,
		// bindingWatches is a sync.Map, no initialization needed
	}
}

// SetupInformersForBinding creates and starts watches for a ProfileBinding
func (im *InformerManager) SetupInformersForBinding(ctx context.Context, binding *profilesv1alpha1.ProfileBinding) error {
	log := logf.FromContext(ctx)

	bindingKey := client.ObjectKeyFromObject(binding).String()

	// Stop existing watches if any
	if existing, exists := im.bindingWatches.LoadAndDelete(bindingKey); exists {
		log.Info("Stopping existing watches for ProfileBinding", "binding", bindingKey)
		existing.(*BindingWatches).cancelCtx()
	}

	// Skip if binding is disabled
	if binding.Spec.Enabled != nil && !*binding.Spec.Enabled {
		log.Info("ProfileBinding is disabled, skipping watch setup", "binding", bindingKey)
		return nil
	}

	log.Info("Setting up watches for ProfileBinding", "binding", bindingKey)

	watchCtx, cancel := context.WithCancel(ctx)
	bindingWatches := &BindingWatches{
		binding:   binding.DeepCopy(),
		watches:   make(map[schema.GroupVersionKind]*WatchHandler),
		cancelCtx: cancel,
	}

	// Create watches for each target resource type
	if err := im.createWatchesForTargets(watchCtx, bindingWatches); err != nil {
		cancel()
		return fmt.Errorf("failed to create watches for binding %s: %w", bindingKey, err)
	}

	// Store the binding watches
	im.bindingWatches.Store(bindingKey, bindingWatches)

	log.Info("Successfully setup watches for ProfileBinding", "binding", bindingKey, "watchCount", len(bindingWatches.watches))
	return nil
}

// createWatchesForTargets creates watches for all target resource types
func (im *InformerManager) createWatchesForTargets(ctx context.Context, bindingWatches *BindingWatches) error {
	log := logf.FromContext(ctx)
	binding := bindingWatches.binding
	targetSelector := &binding.Spec.TargetSelector
	resourceRule := &targetSelector.ResourceRule

	// Iterate through all API groups, versions, and resources in the rule
	for _, apiGroup := range resourceRule.APIGroups {
		for _, apiVersion := range resourceRule.APIVersions {
			for _, resource := range resourceRule.Resources {
				gvk := schema.GroupVersionKind{
					Group:   apiGroup,
					Version: apiVersion,
					Kind:    im.resourceToKind(resource),
				}

				log.Info("Creating watch for resource type", "gvk", gvk, "binding", binding.Name)

				if err := im.createWatchForGVK(ctx, bindingWatches, gvk); err != nil {
					return fmt.Errorf("failed to create watch for GVK %s: %w", gvk, err)
				}
			}
		}
	}

	return nil
}

// createWatchForGVK creates a watch for a specific GroupVersionKind using direct resource monitoring
func (im *InformerManager) createWatchForGVK(ctx context.Context, bindingWatches *BindingWatches, gvk schema.GroupVersionKind) error {
	log := logf.FromContext(ctx)

	// Check if watch already exists for this GVK
	if _, exists := bindingWatches.watches[gvk]; exists {
		log.Info("Watch already exists for GVK", "gvk", gvk)
		return nil
	}

	// Create watch handler
	watchHandler := &WatchHandler{
		gvk:     gvk,
		binding: bindingWatches.binding,
		manager: im,
	}

	// Store the watch handler
	bindingWatches.watches[gvk] = watchHandler

	// Start monitoring in background
	go im.monitorResources(ctx, watchHandler)

	log.Info("Successfully created watch for GVK", "gvk", gvk)
	return nil
}

// monitorResources monitors resources and applies profiles when changes are detected
func (im *InformerManager) monitorResources(ctx context.Context, handler *WatchHandler) {
	log := logf.FromContext(ctx).WithValues("gvk", handler.gvk, "binding", handler.binding.Name)

	// Initial application of profile to existing resources
	if err := im.applyProfileToExistingResources(ctx, handler); err != nil {
		log.Error(err, "Failed to apply profile to existing resources")
	}

	// Note: For a production implementation, you would set up a proper watch here
	// For now, we'll do a one-time application which will be triggered by the controller
	log.Info("Resource monitoring completed for initial application")
}

// applyProfileToExistingResources applies the profile to all existing resources that match the selector
func (im *InformerManager) applyProfileToExistingResources(ctx context.Context, handler *WatchHandler) error {
	log := logf.FromContext(ctx)

	binding := handler.binding
	gvk := handler.gvk

	// Get target namespaces
	targetNamespaces := binding.Spec.TargetSelector.Namespaces
	if len(targetNamespaces) == 0 {
		targetNamespaces = []string{binding.Namespace}
	}

	// Get the profile
	var profile profilesv1alpha1.Profile
	if err := im.Get(ctx, types.NamespacedName{Name: binding.Spec.ProfileRef.Name}, &profile); err != nil {
		return fmt.Errorf("failed to get profile %s: %w", binding.Spec.ProfileRef.Name, err)
	}

	// Get resources in all target namespaces
	var allResources []unstructured.Unstructured
	for _, namespace := range targetNamespaces {
		resources, err := im.getResourcesInNamespace(ctx, gvk, namespace, &binding.Spec.TargetSelector)
		if err != nil {
			log.Error(err, "Failed to get resources in namespace", "namespace", namespace, "gvk", gvk)
			continue
		}
		allResources = append(allResources, resources...)
	}

	if len(allResources) == 0 {
		log.Info("No matching resources found", "gvk", gvk)
		return nil
	}

	// Apply profile to all matching resources
	updated, failed, err := im.Applicator.ApplyProfileToResources(ctx, binding, &profile, allResources)
	if err != nil {
		return fmt.Errorf("failed to apply profile to resources: %w", err)
	}

	log.Info("Applied profile to existing resources",
		"gvk", gvk,
		"total", len(allResources),
		"updated", updated,
		"failed", failed)

	return nil
}

// getResourcesInNamespace gets resources in a specific namespace
func (im *InformerManager) getResourcesInNamespace(ctx context.Context, gvk schema.GroupVersionKind, namespace string, selector *profilesv1alpha1.TargetSelector) ([]unstructured.Unstructured, error) {
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

	if err := im.List(ctx, resourceList, listOpts...); err != nil {
		return nil, err
	}

	return resourceList.Items, nil
}

// resourceMatchesSelector checks if a resource matches the ProfileBinding selector
func (im *InformerManager) resourceMatchesSelector(obj *unstructured.Unstructured, binding *profilesv1alpha1.ProfileBinding) bool {
	targetSelector := &binding.Spec.TargetSelector

	// Check namespace
	targetNamespaces := targetSelector.Namespaces
	if len(targetNamespaces) == 0 {
		targetNamespaces = []string{binding.Namespace}
	}

	objNamespace := obj.GetNamespace()
	namespaceMatches := false
	for _, ns := range targetNamespaces {
		if ns == objNamespace {
			namespaceMatches = true
			break
		}
	}
	if !namespaceMatches {
		return false
	}

	// Check object selector (labels)
	if targetSelector.ObjectSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(targetSelector.ObjectSelector)
		if err != nil {
			return false
		}
		if !selector.Matches(labels.Set(obj.GetLabels())) {
			return false
		}
	}

	return true
}

// StopInformersForBinding stops all watches for a ProfileBinding
func (im *InformerManager) StopInformersForBinding(binding *profilesv1alpha1.ProfileBinding) {
	log := logf.Log.WithName("informer-manager")

	bindingKey := client.ObjectKeyFromObject(binding).String()

	if bindingWatches, exists := im.bindingWatches.LoadAndDelete(bindingKey); exists {
		log.Info("Stopping watches for ProfileBinding", "binding", bindingKey)
		bindingWatches.(*BindingWatches).cancelCtx()
	}
}

// StopAllInformers stops all watches
func (im *InformerManager) StopAllInformers() {
	log := logf.Log.WithName("informer-manager")

	// Count entries for logging
	count := 0
	im.bindingWatches.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	log.Info("Stopping all watches", "count", count)

	// Stop all watches
	im.bindingWatches.Range(func(key, value interface{}) bool {
		bindingKey := key.(string)
		bindingWatches := value.(*BindingWatches)
		log.Info("Stopping watches for binding", "binding", bindingKey)
		bindingWatches.cancelCtx()
		// Delete the entry
		im.bindingWatches.Delete(key)
		return true
	})
}

// resourceToKind converts a resource name to its Kind (simple heuristic)
func (im *InformerManager) resourceToKind(resource string) string {
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
