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

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

var _ = Describe("DNSEndpoint Webhook", func() {
	var (
		obj       *infrastructurev1alpha1.DNSEndpoint
		oldObj    *infrastructurev1alpha1.DNSEndpoint
		validator DNSEndpointCustomValidator
	)

	BeforeEach(func() {
		obj = &infrastructurev1alpha1.DNSEndpoint{}
		oldObj = &infrastructurev1alpha1.DNSEndpoint{}
		scheme := runtime.NewScheme()
		Expect(infrastructurev1alpha1.AddToScheme(scheme)).To(Succeed())
		validator = DNSEndpointCustomValidator{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
		// TODO (user): Add any setup logic common to all tests
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating or updating DNSEndpoint under Validating Webhook", func() {
		It("rejects creation when spec.zone is missing", func() {
			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError("spec.zone is required"))
		})

		It("rejects creation when the referenced zone does not exist", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(`zone "example-zone" does not exist in namespace "default"`))
		})

		It("admits creation when the referenced zone exists in the same namespace", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())

			warnings, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("rejects updates when spec.zone is missing", func() {
			_, err := validator.ValidateUpdate(context.Background(), oldObj, obj)
			Expect(err).To(MatchError("spec.zone is required"))
		})

		It("rejects updates when the referenced zone does not exist", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}

			_, err := validator.ValidateUpdate(context.Background(), oldObj, obj)
			Expect(err).To(MatchError(`zone "example-zone" does not exist in namespace "default"`))
		})

		It("admits updates when the referenced zone exists in the same namespace", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())

			warnings, err := validator.ValidateUpdate(context.Background(), oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("requires routeSelector for weighted, geolocation, and round-robin routing on create and update", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())

			for _, policy := range []string{"Weighted", "Geolocation", "RoundRobin"} {
				obj.Spec.RoutingPolicy = policy
				_, createErr := validator.ValidateCreate(context.Background(), obj)
				Expect(createErr).To(MatchError("spec.routeSelector is required when routingPolicy is \"" + policy + "\""))

				_, updateErr := validator.ValidateUpdate(context.Background(), oldObj, obj)
				Expect(updateErr).To(MatchError("spec.routeSelector is required when routingPolicy is \"" + policy + "\""))
			}
		})
	})

})
