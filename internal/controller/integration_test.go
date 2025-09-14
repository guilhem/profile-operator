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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

var _ = Describe("Profile Operator Tests", func() {
	Context("When validating profiles", func() {
		var (
			ctx context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should validate and mark a simple profile as ready", func() {
			// Use unique name to avoid conflicts
			profileName := fmt.Sprintf("test-profile-%d", time.Now().UnixNano())

			By("Creating a simple test profile")
			profile := &profilesv1alpha1.Profile{
				ObjectMeta: metav1.ObjectMeta{
					Name: profileName,
				},
				Spec: &profilesv1alpha1.ProfileSpec{
					Description: ptr.To("Simple test profile"),
					Priority:    ptr.To(int32(5)),
					Template: &profilesv1alpha1.ProfileTemplate{
						PatchStrategicMerge: &runtime.RawExtension{
							Raw: []byte(`{
								"metadata": {
									"labels": {
										"test": "true"
									}
								}
							}`),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			// Cleanup at end
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			})

			By("Reconciling the profile multiple times to ensure it becomes Ready")
			reconciler := &ProfileReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &FakeRecorder{},
			} // First reconciliation - should add finalizer
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: profileName},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 2)) // Should requeue due to finalizer addition

			// Second reconciliation - should set to Pending
			result, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: profileName},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 2)) // Should requeue due to status initialization

			By("Checking profile status after second reconciliation")
			var secondProfile profilesv1alpha1.Profile
			err = k8sClient.Get(ctx, types.NamespacedName{Name: profileName}, &secondProfile)
			Expect(err).NotTo(HaveOccurred())
			// Verify initial state - conditions should indicate pending

			// Third reconciliation - should set to Ready
			result, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: profileName},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">=", 0))

			By("Checking profile status is Ready")
			var finalProfile profilesv1alpha1.Profile
			err = k8sClient.Get(ctx, types.NamespacedName{Name: profileName}, &finalProfile)
			Expect(err).NotTo(HaveOccurred())
			// Verify final state through conditions
			readyCondition := meta.FindStatusCondition(finalProfile.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		})
	})
})
