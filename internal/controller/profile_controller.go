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
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
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

// Profile condition types
const (
	ProfileConditionReady = "Ready"
	ProfileConditionValid = "Valid"
)

// Profile condition reasons
const (
	ProfileReasonPending             = "Pending"
	ProfileReasonValidationSucceeded = "ValidationSucceeded"
	ProfileReasonValidationFailed    = "ValidationFailed"
)

// ProfileReconciler reconciles a Profile object
type ProfileReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Finalizer name for Profile
const ProfileFinalizer = "profile.barpilot.io/finalizer"

// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=profiles.barpilot.io,resources=profiles/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Profile resource
	var profile profilesv1alpha1.Profile
	if err := r.Get(ctx, req.NamespacedName, &profile); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Profile resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Profile")
		return ctrl.Result{}, err
	}

	// Handle deletion or normal reconciliation
	if !profile.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &profile)
	}
	return r.reconcileNormal(ctx, &profile)
}

// reconcileDelete handles the deletion of the Profile resource
func (r *ProfileReconciler) reconcileDelete(ctx context.Context, profile *profilesv1alpha1.Profile) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if our finalizer is present
	if !controllerutil.ContainsFinalizer(profile, ProfileFinalizer) {
		// No finalizer means we don't need to do cleanup
		return ctrl.Result{}, nil
	}

	log.Info("Performing cleanup for Profile deletion", "name", profile.Name)

	// Perform cleanup tasks here
	// For now, we don't have any specific cleanup for Profile resources
	// since they are just templates used by ProfileBindings

	// Remove our finalizer
	op, err := controllerutil.CreateOrPatch(ctx, r.Client, profile, func() error {
		controllerutil.RemoveFinalizer(profile, ProfileFinalizer)
		return nil
	})
	if err != nil {
		log.Error(err, "Failed to remove finalizer")
		r.Recorder.Event(profile, corev1.EventTypeWarning, "FinalizerRemovalFailed", fmt.Sprintf("Failed to remove finalizer: %v", err))
		return ctrl.Result{}, err
	}
	if op != controllerutil.OperationResultNone {
		r.Recorder.Event(profile, corev1.EventTypeNormal, "FinalizerRemoved", "Finalizer removed successfully")
	}

	log.Info("Successfully completed Profile deletion", "name", profile.Name)
	return ctrl.Result{}, nil
}

// reconcileNormal handles normal reconciliation when resource is not being deleted
func (r *ProfileReconciler) reconcileNormal(ctx context.Context, profile *profilesv1alpha1.Profile) (ctrl.Result, error) {
	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(profile, ProfileFinalizer) {
		op, err := controllerutil.CreateOrPatch(ctx, r.Client, profile, func() error {
			controllerutil.AddFinalizer(profile, ProfileFinalizer)
			return nil
		})
		if err != nil {
			r.Recorder.Event(profile, corev1.EventTypeWarning, "FinalizerFailed", fmt.Sprintf("Failed to add finalizer: %v", err))
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		if op != controllerutil.OperationResultNone {
			r.Recorder.Event(profile, corev1.EventTypeNormal, "FinalizerAdded", "Finalizer added successfully")
		}
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}

	// Initialize conditions if needed
	if len(profile.Status.Conditions) == 0 {
		r.initializeProfileConditions(profile)
		profile.Status.ObservedGeneration = &profile.Generation
		// Update status and requeue
		r.updateProfileStatus(ctx, profile)
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}

	// Validate the profile
	if err := r.validateProfile(profile); err != nil {
		r.markProfileAsInvalid(profile, err)
		r.Recorder.Event(profile, corev1.EventTypeWarning, "ValidationFailed", fmt.Sprintf("Profile validation failed: %v", err))
		// Update status before returning
		r.updateProfileStatus(ctx, profile)
		return ctrl.Result{}, nil
	}

	// Mark as valid and ready
	r.markProfileAsReady(profile)
	r.Recorder.Event(profile, corev1.EventTypeNormal, "ValidationSucceeded", "Profile validation completed successfully")

	// Always update status at the end
	r.updateProfileStatus(ctx, profile)
	return ctrl.Result{}, nil
} // updateProfileStatus centralizes status updates for profiles
func (r *ProfileReconciler) updateProfileStatus(ctx context.Context, profile *profilesv1alpha1.Profile) {
	log := logf.FromContext(ctx)

	// Fetch the latest version of the profile to ensure we have the correct resource version
	var latestProfile profilesv1alpha1.Profile
	if err := r.Get(ctx, types.NamespacedName{Name: profile.Name, Namespace: profile.Namespace}, &latestProfile); err != nil {
		log.Error(err, "Failed to get latest profile for status update")
		return
	}

	// Copy the status fields from our working copy to the latest version
	latestProfile.Status = profile.Status

	if err := r.Status().Update(ctx, &latestProfile); err != nil {
		log.Error(err, "Failed to update status")
	}
}

// markProfileAsInvalid marks a profile as invalid due to validation failure
func (r *ProfileReconciler) markProfileAsInvalid(profile *profilesv1alpha1.Profile, err error) {
	r.setProfileCondition(profile, ProfileConditionReady, metav1.ConditionFalse, ProfileReasonValidationFailed, fmt.Sprintf("Validation failed: %v", err))
	r.setProfileCondition(profile, ProfileConditionValid, metav1.ConditionFalse, ProfileReasonValidationFailed, err.Error())
}

// markProfileAsReady marks a profile as ready and valid
func (r *ProfileReconciler) markProfileAsReady(profile *profilesv1alpha1.Profile) {
	r.setProfileCondition(profile, ProfileConditionReady, metav1.ConditionTrue, ProfileReasonValidationSucceeded, "Profile is ready for use")
	r.setProfileCondition(profile, ProfileConditionValid, metav1.ConditionTrue, ProfileReasonValidationSucceeded, "Profile template is valid")
}

// validateProfile validates the profile template structure
func (r *ProfileReconciler) validateProfile(profile *profilesv1alpha1.Profile) error {
	// Validate template has overlay configuration
	if profile.Spec.Template.RawPatchStrategicMerge == nil || profile.Spec.Template.RawPatchStrategicMerge.Raw == nil {
		return fmt.Errorf("template must contain overlay configuration")
	}

	// Validate overlay template JSON
	var temp interface{}
	if err := json.Unmarshal(profile.Spec.Template.RawPatchStrategicMerge.Raw, &temp); err != nil {
		return fmt.Errorf("invalid JSON in template overlay: %v", err)
	}

	// Additional validation can be added here
	// For example, checking for required fields, validating specific structures, etc.

	return nil
}

// initializeProfileConditions sets up initial conditions for a Profile
func (r *ProfileReconciler) initializeProfileConditions(profile *profilesv1alpha1.Profile) {
	initialConditions := []metav1.Condition{
		{
			Type:    ProfileConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  ProfileReasonPending,
			Message: "Profile validation is pending",
		},
		{
			Type:    ProfileConditionValid,
			Status:  metav1.ConditionUnknown,
			Reason:  ProfileReasonPending,
			Message: "Profile validation has not been performed yet",
		},
	}

	for _, condition := range initialConditions {
		meta.SetStatusCondition(&profile.Status.Conditions, condition)
	}
}

// setProfileCondition updates or creates a condition for the Profile
func (r *ProfileReconciler) setProfileCondition(profile *profilesv1alpha1.Profile, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}

	meta.SetStatusCondition(&profile.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&profilesv1alpha1.Profile{}).
		Complete(r)
}
