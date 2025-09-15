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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

const (
	profileBindingFinalizer = "profilebinding.profiles.barpilot.io/finalizer"

	// Condition types - keep it simple for our use case
	ConditionReady = "Ready" // ProfileBinding has successfully applied the profile

	// Condition reasons
	ReasonApplied          = "Applied"
	ReasonFailed           = "Failed"
	ReasonInitializing     = "Initializing"
	ReasonDisabled         = "Disabled"
	ReasonProfileNotFound  = "ProfileNotFound"
	ReasonPartiallyApplied = "PartiallyApplied"
	ReasonNoTargetsFound   = "NoTargetsFound"
)

// ProfileBindingReconciler reconciles a ProfileBinding object
type ProfileBindingReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Agent    *ProfileBindingAgent
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
			// Handle deletion through agent
			log.Info("ProfileBinding not found, likely deleted", "binding", req.NamespacedName)
			// Create a minimal ProfileBinding object for deletion handling
			deletedBinding := &profilesv1alpha1.ProfileBinding{}
			deletedBinding.SetName(req.Name)
			deletedBinding.SetNamespace(req.Namespace)
			return ctrl.Result{}, r.Agent.HandleProfileBindingChange(ctx, deletedBinding, true)
		}
		return ctrl.Result{}, err
	}

	// Always update status at the end
	defer r.updateStatus(ctx, &profileBinding)

	// Handle deletion
	if !profileBinding.DeletionTimestamp.IsZero() {
		log.Info("ProfileBinding is being deleted", "binding", req.NamespacedName)

		// Notify agent about deletion
		if err := r.Agent.HandleProfileBindingChange(ctx, &profileBinding, true); err != nil {
			log.Error(err, "Failed to handle ProfileBinding deletion in agent")
			// Continue with finalizer removal even if agent fails
		}

		// Remove finalizer and let the resource be deleted
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
		// Don't return here, continue with agent setup
	}

	// Delegate to agent for informer management
	if err := r.Agent.HandleProfileBindingChange(ctx, &profileBinding, false); err != nil {
		log.Error(err, "Failed to handle ProfileBinding change in agent")
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonFailed, err.Error())
		return ctrl.Result{}, err
	}

	// Set initial ready condition if not disabled
	if profileBinding.Spec.Enabled != nil && !*profileBinding.Spec.Enabled {
		r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonDisabled, "ProfileBinding is disabled")
	} else {
		// Check if profile exists
		var profile profilesv1alpha1.Profile
		if err := r.Get(ctx, types.NamespacedName{Name: profileBinding.Spec.ProfileRef.Name}, &profile); err != nil {
			r.setCondition(&profileBinding, ConditionReady, metav1.ConditionFalse, ReasonProfileNotFound, err.Error())
		} else {
			r.setCondition(&profileBinding, ConditionReady, metav1.ConditionTrue, ReasonApplied, "ProfileBinding is active and monitoring target resources")
		}
	}

	// No requeue needed - informers will handle resource changes automatically
	return ctrl.Result{}, nil
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
