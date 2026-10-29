/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package v1alpha1

import (
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	// TODO (user): Add any additional imports if needed
)

var _ = Describe("Project Webhook", func() {
	var (
		obj       *infrastructurev1alpha1.Project
		oldObj    *infrastructurev1alpha1.Project
		validator ProjectCustomValidator
	)

	BeforeEach(func() {
		obj = &infrastructurev1alpha1.Project{}
		oldObj = &infrastructurev1alpha1.Project{}
		validator = ProjectCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
		// TODO (user): Add any setup logic common to all tests
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating or updating Project under Validating Webhook", func() {
		It("rejects absent, null, and empty ownership through the CRD schema", func() {
			for _, spec := range []interface{}{nil, map[string]interface{}{}, map[string]interface{}{"project": nil}, map[string]interface{}{"project": map[string]interface{}{}}} {
				resource := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": infrastructurev1alpha1.SchemeGroupVersion.String(), "kind": "APIKey",
					"metadata": map[string]interface{}{"name": "invalid-ownership", "namespace": "default"},
				}}
				if spec != nil {
					resource.Object["spec"] = spec
				}
				err := k8sClient.Create(ctx, resource)
				Expect(apierrors.IsInvalid(err)).To(BeTrue(), "%v", err)
			}
		})

		It("admits owned resources and rejects reassignment and foreign project names", func() {
			project := &infrastructurev1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"}}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, project)).To(Succeed()) })
			key := &infrastructurev1alpha1.APIKey{ObjectMeta: metav1.ObjectMeta{Name: "owned-key", Namespace: "default"}, Spec: infrastructurev1alpha1.APIKeySpec{Project: &corev1.LocalObjectReference{Name: "default"}}}
			Expect(k8sClient.Create(ctx, key)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, &infrastructurev1alpha1.APIKey{ObjectMeta: metav1.ObjectMeta{Name: "owned-key", Namespace: "default"}})).To(Succeed())
			})
			key.Spec.Project.Name = "other"
			Expect(k8sClient.Update(ctx, key)).NotTo(Succeed())
			key.Name = "foreign-key"
			key.ResourceVersion = ""
			key.UID = ""
			Expect(k8sClient.Create(ctx, key)).NotTo(Succeed())
		})
		It("requires the project name to match its namespace", func() {
			project := &infrastructurev1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: "wrong-name", Namespace: "default"}}
			Expect(k8sClient.Create(ctx, project)).NotTo(Succeed())
		})

		It("allows only one project even during concurrent creation", func() {
			errors := make(chan error, 2)
			var workers sync.WaitGroup
			for range 2 {
				workers.Go(func() {
					errors <- k8sClient.Create(ctx, &infrastructurev1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"}})
				})
			}
			workers.Wait()
			close(errors)
			successes := 0
			for err := range errors {
				if err == nil {
					successes++
				} else {
					Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "%v", err)
				}
			}
			Expect(successes).To(Equal(1))
			Expect(k8sClient.Delete(ctx, &infrastructurev1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"}})).To(Succeed())
		})
		// TODO (user): Add logic for validating webhooks
		// Example:
		// It("Should deny creation if a required field is missing", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = ""
		//     Expect(validator.ValidateCreate(ctx, obj)).Error().To(HaveOccurred())
		// })
		//
		// It("Should admit creation if all required fields are present", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = "valid_value"
		//     Expect(validator.ValidateCreate(ctx, obj)).To(BeNil())
		// })
		//
		// It("Should validate updates correctly", func() {
		//     By("simulating a valid update scenario")
		//     oldObj.SomeRequiredField = "updated_value"
		//     obj.SomeRequiredField = "updated_value"
		//     Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())
		// })
	})

})
