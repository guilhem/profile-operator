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
	"sync"

	v1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

// ProfileBindingAgent manages the lifecycle of informers for ProfileBindings
// It acts as an agent that automatically sets up and tears down informers based on ProfileBinding changes
type ProfileBindingAgent struct {
	client.Client
	Recorder        record.EventRecorder
	InformerManager *InformerManager
	manager         ctrl.Manager

	// Internal state
	activeBindings map[string]*profilesv1alpha1.ProfileBinding
	mutex          sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewProfileBindingAgent creates a new ProfileBindingAgent
func NewProfileBindingAgent(client client.Client, recorder record.EventRecorder, mgr ctrl.Manager) *ProfileBindingAgent {
	applicator := NewProfileApplicator(client, recorder)
	informerManager := NewInformerManager(client, recorder, applicator, mgr)

	ctx, cancel := context.WithCancel(context.Background())

	return &ProfileBindingAgent{
		Client:          client,
		Recorder:        recorder,
		InformerManager: informerManager,
		manager:         mgr,
		activeBindings:  make(map[string]*profilesv1alpha1.ProfileBinding),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start starts the agent and begins managing ProfileBinding informers
func (agent *ProfileBindingAgent) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("profile-binding-agent")
	log.Info("Starting ProfileBinding agent")

	// Update context
	agent.ctx = ctx

	// Initialize informers for existing ProfileBindings
	if err := agent.initializeExistingBindings(); err != nil {
		return fmt.Errorf("failed to initialize existing bindings: %w", err)
	}

	log.Info("ProfileBinding agent started successfully")

	// Wait for context cancellation
	<-ctx.Done()
	log.Info("ProfileBinding agent context cancelled, stopping")

	// Cleanup
	agent.Stop()

	return nil
}

// Stop stops the agent and cleans up all informers
func (agent *ProfileBindingAgent) Stop() {
	log := logf.Log.WithName("profile-binding-agent")
	log.Info("Stopping ProfileBinding agent")

	// Cancel context
	agent.cancel()

	// Stop all informers
	agent.InformerManager.StopAllInformers()

	// Clear active bindings
	agent.mutex.Lock()
	agent.activeBindings = make(map[string]*profilesv1alpha1.ProfileBinding)
	agent.mutex.Unlock()

	log.Info("ProfileBinding agent stopped")
}

// HandleProfileBindingChange handles changes to ProfileBinding resources
func (agent *ProfileBindingAgent) HandleProfileBindingChange(ctx context.Context, binding *profilesv1alpha1.ProfileBinding, isDelete bool) error {
	bindingKey := client.ObjectKeyFromObject(binding).String()

	if isDelete {
		return agent.handleProfileBindingDelete(ctx, binding, bindingKey)
	}

	return agent.handleProfileBindingCreateOrUpdate(ctx, binding, bindingKey)
}

// handleProfileBindingDelete handles deletion of a ProfileBinding
func (agent *ProfileBindingAgent) handleProfileBindingDelete(ctx context.Context, binding *profilesv1alpha1.ProfileBinding, bindingKey string) error {
	log := logf.FromContext(ctx)
	log.Info("Handling ProfileBinding deletion", "binding", bindingKey)

	agent.mutex.Lock()
	defer agent.mutex.Unlock()

	// Remove from active bindings
	delete(agent.activeBindings, bindingKey)

	// Stop informers for this binding (if InformerManager exists)
	if agent.InformerManager != nil {
		agent.InformerManager.StopInformersForBinding(binding)
	}

	log.Info("Successfully handled ProfileBinding deletion", "binding", bindingKey)
	return nil
}

// handleProfileBindingCreateOrUpdate handles creation or update of a ProfileBinding
func (agent *ProfileBindingAgent) handleProfileBindingCreateOrUpdate(ctx context.Context, binding *profilesv1alpha1.ProfileBinding, bindingKey string) error {
	log := logf.FromContext(ctx)

	agent.mutex.Lock()
	defer agent.mutex.Unlock()

	// Check if this is a meaningful change
	if agent.hasBindingChanged(binding, bindingKey) {
		log.Info("ProfileBinding has changed, updating informers", "binding", bindingKey)

		// Stop existing informers (if InformerManager exists)
		if agent.InformerManager != nil {
			agent.InformerManager.StopInformersForBinding(binding)

			// Set up new informers
			if err := agent.InformerManager.SetupInformersForBinding(ctx, binding); err != nil {
				log.Error(err, "Failed to setup informers for ProfileBinding", "binding", bindingKey)
				return err
			}
		}

		// Update active bindings
		agent.activeBindings[bindingKey] = binding.DeepCopy()

		log.Info("Successfully updated informers for ProfileBinding", "binding", bindingKey)
	} else {
		log.Info("ProfileBinding has not changed significantly, keeping existing informers", "binding", bindingKey)
	}

	return nil
}

// hasBindingChanged checks if a ProfileBinding has changed in a way that affects informers
func (agent *ProfileBindingAgent) hasBindingChanged(newBinding *profilesv1alpha1.ProfileBinding, bindingKey string) bool {
	existingBinding, exists := agent.activeBindings[bindingKey]
	if !exists {
		return true // New binding
	}

	// Check if enabled status changed
	if (newBinding.Spec.Enabled == nil) != (existingBinding.Spec.Enabled == nil) {
		return true
	}
	if newBinding.Spec.Enabled != nil && existingBinding.Spec.Enabled != nil {
		if *newBinding.Spec.Enabled != *existingBinding.Spec.Enabled {
			return true
		}
	}

	// Check if target selector changed
	if !agent.targetSelectorsEqual(&newBinding.Spec.TargetSelector, &existingBinding.Spec.TargetSelector) {
		return true
	}

	// Check if profile reference changed
	if newBinding.Spec.ProfileRef.Name != existingBinding.Spec.ProfileRef.Name {
		return true
	}

	return false
}

// targetSelectorsEqual compares two TargetSelectors for equality
func (agent *ProfileBindingAgent) targetSelectorsEqual(a, b *profilesv1alpha1.TargetSelector) bool {
	// Compare namespaces
	if len(a.Namespaces) != len(b.Namespaces) {
		return false
	}
	for i, ns := range a.Namespaces {
		if i >= len(b.Namespaces) || ns != b.Namespaces[i] {
			return false
		}
	}

	// Compare resource rules
	if !agent.resourceRulesEqual(&a.ResourceRule, &b.ResourceRule) {
		return false
	}

	// Compare object selectors (basic comparison)
	if (a.ObjectSelector == nil) != (b.ObjectSelector == nil) {
		return false
	}
	if a.ObjectSelector != nil && b.ObjectSelector != nil {
		// For simplicity, we'll consider any change to object selector as significant
		// A more sophisticated comparison could be implemented if needed
		if len(a.ObjectSelector.MatchLabels) != len(b.ObjectSelector.MatchLabels) {
			return false
		}
		for key, value := range a.ObjectSelector.MatchLabels {
			if b.ObjectSelector.MatchLabels[key] != value {
				return false
			}
		}
	}

	return true
}

// resourceRulesEqual compares two ResourceRules for equality
func (agent *ProfileBindingAgent) resourceRulesEqual(a, b *v1.Rule) bool {
	// Compare API groups
	if len(a.APIGroups) != len(b.APIGroups) {
		return false
	}
	for i, group := range a.APIGroups {
		if i >= len(b.APIGroups) || group != b.APIGroups[i] {
			return false
		}
	}

	// Compare API versions
	if len(a.APIVersions) != len(b.APIVersions) {
		return false
	}
	for i, version := range a.APIVersions {
		if i >= len(b.APIVersions) || version != b.APIVersions[i] {
			return false
		}
	}

	// Compare resources
	if len(a.Resources) != len(b.Resources) {
		return false
	}
	for i, resource := range a.Resources {
		if i >= len(b.Resources) || resource != b.Resources[i] {
			return false
		}
	}

	return true
}

// initializeExistingBindings initializes informers for existing ProfileBindings
func (agent *ProfileBindingAgent) initializeExistingBindings() error {
	log := logf.Log.WithName("profile-binding-agent")

	// List all existing ProfileBindings
	var bindingList profilesv1alpha1.ProfileBindingList
	if err := agent.List(agent.ctx, &bindingList); err != nil {
		return fmt.Errorf("failed to list existing ProfileBindings: %w", err)
	}

	log.Info("Initializing informers for existing ProfileBindings", "count", len(bindingList.Items))

	// Set up informers for each binding
	for _, binding := range bindingList.Items {
		bindingCopy := binding.DeepCopy()
		if err := agent.HandleProfileBindingChange(agent.ctx, bindingCopy, false); err != nil {
			log.Error(err, "Failed to initialize informers for ProfileBinding", "binding", binding.Name)
			// Continue with other bindings even if one fails
		}
	}

	return nil
}

// GetActiveBindings returns a copy of currently active ProfileBindings
func (agent *ProfileBindingAgent) GetActiveBindings() map[string]*profilesv1alpha1.ProfileBinding {
	agent.mutex.RLock()
	defer agent.mutex.RUnlock()

	result := make(map[string]*profilesv1alpha1.ProfileBinding)
	for key, binding := range agent.activeBindings {
		result[key] = binding.DeepCopy()
	}

	return result
}

// GetInformerManager returns the InformerManager instance
func (agent *ProfileBindingAgent) GetInformerManager() *InformerManager {
	return agent.InformerManager
}
