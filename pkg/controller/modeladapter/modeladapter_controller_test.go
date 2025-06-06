/*
Copyright 2024 The Aibrix Team.

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

package modeladapter

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	modelv1alpha1 "github.com/vllm-project/aibrix/api/model/v1alpha1"
)

var _ = Describe("ModelAdapter Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		modeladapter := &modelv1alpha1.ModelAdapter{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ModelAdapter")
			err := k8sClient.Get(ctx, typeNamespacedName, modeladapter)
			if err != nil && errors.IsNotFound(err) {
				resource := &modelv1alpha1.ModelAdapter{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &modelv1alpha1.ModelAdapter{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ModelAdapter")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		//It("should successfully reconcile the resource", func() {
		//	By("Reconciling the created resource")
		//	controllerReconciler := &ModelAdapterReconciler{
		//		Client: k8sClient,
		//		Scheme: k8sClient.Scheme(),
		//	}
		//
		//	_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
		//		NamespacedName: typeNamespacedName,
		//	})
		//	Expect(err).NotTo(HaveOccurred())
		//	// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
		//	// Example: If you expect a certain status condition after reconciliation, verify it here.
		//})
	})
})
