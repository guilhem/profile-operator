package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kyverno/kyverno/pkg/engine/mutate/patch"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

// ProfileApplicator handles the application of profiles to target resources
// This is a service component, not a controller, to avoid controller-runtime complexities
type ProfileApplicator struct {
	client.Client
	Recorder record.EventRecorder
}

// Event reasons for target resources
const (
	EventReasonProfileApplied           = "ProfileApplied"
	EventReasonProfileApplicationFailed = "ProfileApplicationFailed"
	EventReasonProfileUpToDate          = "ProfileUpToDate"
)

// NewProfileApplicator creates a new ProfileApplicator
func NewProfileApplicator(cClient client.Client, recorder record.EventRecorder) *ProfileApplicator {
	return &ProfileApplicator{
		Client:   cClient,
		Recorder: recorder,
	}
}

// ApplyProfileToResources applies a profile to multiple target resources
func (a *ProfileApplicator) ApplyProfileToResources(ctx context.Context,
	binding *profilesv1alpha1.ProfileBinding,
	profile *profilesv1alpha1.Profile,
	targets []unstructured.Unstructured) (updated, failed int, err error) {

	log := logf.FromContext(ctx)

	for i, target := range targets {
		log.Info("Applying profile to resource",
			"index", i,
			"resource", target.GetName(),
			"kind", target.GetKind(),
			"profile", profile.Name)

		if err := a.applyProfileToResource(ctx, profile, &target); err != nil {
			log.Error(err, "Failed to apply profile", "resource", target.GetName())
			// Event on ProfileBinding
			a.Recorder.Eventf(binding, "Warning", "ApplicationFailed",
				"Failed to apply profile %s to %s/%s: %v",
				profile.Name, target.GetKind(), target.GetName(), err)
			// Event on target resource
			a.Recorder.Eventf(&target, "Warning", EventReasonProfileApplicationFailed,
				"Profile-operator failed to apply profile %s: %v",
				profile.Name, err)
			failed++
		} else {
			log.Info("Successfully applied profile to resource", "resource", target.GetName())
			// Event on ProfileBinding
			a.Recorder.Eventf(binding, "Normal", "Applied",
				"Successfully applied profile %s to %s/%s",
				profile.Name, target.GetKind(), target.GetName())
			// Event on target resource
			a.Recorder.Eventf(&target, "Normal", EventReasonProfileApplied,
				"Profile-operator successfully applied profile %s",
				profile.Name)
			updated++
		}
	}

	return updated, failed, nil
}

// applyProfileToResource applies a profile to a single target resource
func (a *ProfileApplicator) applyProfileToResource(ctx context.Context,
	profile *profilesv1alpha1.Profile,
	target *unstructured.Unstructured) error {

	if profile.Spec.Template.RawPatchStrategicMerge == nil {
		return fmt.Errorf("profile does not have a valid patchStrategicMerge defined")
	}

	patchJSON := *profile.Spec.Template.RawPatchStrategicMerge

	switch *profile.Spec.ApplyStrategy {
	case profilesv1alpha1.ApplyStrategySSA:
		return a.applyProfileSSA(ctx, patchJSON, target)
	case profilesv1alpha1.ApplyStrategyPatch:
		return a.applyProfileServerPatch(ctx, patchJSON, target)
	default:
		return fmt.Errorf("unknown apply strategy: %s", *profile.Spec.ApplyStrategy)
	}
}

// applyProfileSSA applies a profile using Server-Side Apply with Kyverno strategic merge
func (a *ProfileApplicator) applyProfileSSA(ctx context.Context, patchJSON v1.JSON, target *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)

	log.Info("Applying profile using SSA strategy", "patchSize", len(patchJSON.Raw))

	// Create Kyverno strategic merge patch processor
	patcher := patch.NewPatchStrategicMerge(patchJSON)

	// Convert target.Object to []byte for Kyverno
	targetBytes, err := json.Marshal(target.Object)
	if err != nil {
		return fmt.Errorf("failed to marshal target resource: %w", err)
	}

	// Apply the patch
	patchedBytes, err := patcher.Patch(log, targetBytes)
	if err != nil {
		return fmt.Errorf("failed to apply strategic merge patch: %w", err)
	}

	// Check if the patch would actually change anything
	if strings.TrimSpace(string(targetBytes)) == strings.TrimSpace(string(patchedBytes)) {
		log.Info("Resource is already up-to-date, no changes needed")
		return nil // No changes needed
	}

	// Convert the patched bytes back to map[string]interface{}
	var patchedObject map[string]interface{}
	if err := json.Unmarshal(patchedBytes, &patchedObject); err != nil {
		return fmt.Errorf("failed to unmarshal patched resource: %w", err)
	}

	// Update the target with patched content
	target.Object = patchedObject

	// Apply the changes using Server-Side Apply
	if err := a.Apply(ctx, client.ApplyConfigurationFromUnstructured(target),
		client.FieldOwner("profile-operator")); err != nil {
		return fmt.Errorf("failed to apply resource using SSA: %w", err)
	}

	log.Info("Successfully applied profile using SSA")
	return nil
}

// applyProfileServerPatch applies a profile using server-side strategic merge patch
func (a *ProfileApplicator) applyProfileServerPatch(ctx context.Context, patchJSON v1.JSON, target *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)

	log.Info("Applying profile using server patch strategy", "patchSize", len(patchJSON.Raw))

	// Send the rawPatch as-is to the server using strategic merge rawPatch
	rawPatch := client.RawPatch(types.StrategicMergePatchType, patchJSON.Raw)

	if err := a.Patch(ctx, target, rawPatch, client.FieldOwner("profile-operator")); err != nil {
		return fmt.Errorf("failed to apply server patch to resource: %w", err)
	}

	log.Info("Successfully applied server patch to resource")
	return nil
}
