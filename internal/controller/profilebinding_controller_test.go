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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

var _ = Describe("ProfileBinding Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		profilebinding := &profilesv1alpha1.ProfileBinding{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ProfileBinding")
			err := k8sClient.Get(ctx, typeNamespacedName, profilebinding)
			if err != nil && errors.IsNotFound(err) {
				resource := &profilesv1alpha1.ProfileBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: profilesv1alpha1.ProfileBindingSpec{
						ProfileRef: &profilesv1alpha1.ProfileReference{
							Name: "test-profile",
						},
						TargetSelector: profilesv1alpha1.TargetSelector{
							ResourceRule: admissionregistrationv1.Rule{
								APIGroups:   []string{"apps"},
								APIVersions: []string{"v1"},
								Resources:   []string{"deployments"},
							},
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
						UpdateStrategy: profilesv1alpha1.UpdateStrategy{
							Type: profilesv1alpha1.ImmediateUpdate,
						},
						Enabled: ptr.To(true),
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &profilesv1alpha1.ProfileBinding{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ProfileBinding")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ProfileBindingReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &FakeRecorder{},
			}

			By("Performing initial reconciliation to add finalizer")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// First reconciliation should requeue to add finalizer
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			By("Verifying finalizer was added")
			var updatedBinding profilesv1alpha1.ProfileBinding
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updatedBinding)).To(Succeed())
			Expect(updatedBinding.Finalizers).To(ContainElement("profilebinding.profiles.barpilot.io/finalizer"))

			By("Performing subsequent reconciliations until conditions are set")
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				if err != nil {
					return false
				}

				// Get the latest ProfileBinding
				var currentBinding profilesv1alpha1.ProfileBinding
				if err := k8sClient.Get(ctx, typeNamespacedName, &currentBinding); err != nil {
					return false
				}

				// Check if conditions are initialized
				return len(currentBinding.Status.Conditions) > 0
			}).Should(BeTrue(), "ProfileBinding should have status conditions initialized")

			By("Verifying final ProfileBinding status")
			var finalBinding profilesv1alpha1.ProfileBinding
			Expect(k8sClient.Get(ctx, typeNamespacedName, &finalBinding)).To(Succeed())

			// Verify status conditions exist
			Expect(finalBinding.Status.Conditions).NotTo(BeEmpty())

			// Check for Ready condition - could be either Initializing or ProfileNotFound
			readyCondition := meta.FindStatusCondition(finalBinding.Status.Conditions, ConditionReady)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			// The reason could be either Initializing (if no resources found) or ProfileNotFound (if profile doesn't exist)
			Expect(readyCondition.Reason).To(Or(Equal(ReasonInitializing), Equal(ReasonProfileNotFound)))

			// Verify observed generation is set
			Expect(finalBinding.Status.ObservedGeneration).NotTo(BeNil())
			Expect(*finalBinding.Status.ObservedGeneration).To(Equal(finalBinding.Generation))

			// Verify resource counts are initialized (should be nil since no resources found or processed)
			Expect(finalBinding.Status.TargetedResources).To(BeNil()) // Not set when no resources found
			Expect(finalBinding.Status.UpdatedResources).To(BeNil())
			Expect(finalBinding.Status.FailedResources).To(BeNil())

			// LastUpdated is only set when resources are actually processed
			// In this case, since we're in Initializing state, it may be nil
			// This is expected behavior for this test scenario
		})
	})
})
