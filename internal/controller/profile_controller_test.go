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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	profilesv1alpha1 "github.com/guilhem/profile-operator/api/v1alpha1"
)

var _ = Describe("Profile Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
			// Profile is cluster-scoped, no namespace needed
		}
		profile := &profilesv1alpha1.Profile{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Profile")
			err := k8sClient.Get(ctx, typeNamespacedName, profile)
			if err != nil && errors.IsNotFound(err) {
				resource := &profilesv1alpha1.Profile{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
						// Profile is cluster-scoped, no namespace
					},
					Spec: &profilesv1alpha1.ProfileSpec{
						Description: ptr.To("Test profile for unit testing"),
						Priority:    ptr.To(int32(5)),
						Template: &profilesv1alpha1.ProfileTemplate{
							PatchStrategicMerge: createMetadataOnlyOverlay(map[string]string{
								"test": "true",
							}),
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &profilesv1alpha1.Profile{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Profile")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ProfileReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &FakeRecorder{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
