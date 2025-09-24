// /*
// Copyright 2025.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package controller

import (
	"os"
	"time"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

var _ = Describe("Service Controller", func() {
	Context("When reconciling a resource", func() {

		const (
			StaticServiceName = "static.cdn.edgecdnx.com"
			Namespace         = "default"
			timeout           = time.Second * 10
			interval          = time.Millisecond * 500
			ControllerRole    = "controller"
		)

		serviceLookupKey := types.NamespacedName{Name: StaticServiceName, Namespace: Namespace}
		service := &infrastructurev1alpha1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      StaticServiceName,
				Namespace: Namespace,
			},
			Spec: infrastructurev1alpha1.ServiceSpec{
				Name:       StaticServiceName,
				Domain:     StaticServiceName,
				OriginType: "static",
				StaticOrigins: []infrastructurev1alpha1.StaticOriginSpec{
					{
						Upstream:   "randomupstream.com",
						Port:       443,
						HostHeader: "randomupstream.com",
						Scheme:     "Https",
					},
				},
			},
		}

		BeforeEach(func() {
			role := os.Getenv("ROLE")
			if role != ControllerRole {
				Skip("Skipping test because ROLE is not 'controller'")
			}

			newService := &infrastructurev1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      StaticServiceName,
					Namespace: Namespace,
				},
				Spec: service.Spec,
			}

			Expect(k8sClient.Create(ctx, newService)).To(Succeed())
			createdService := &infrastructurev1alpha1.Service{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceLookupKey, createdService)).To(Succeed())
				g.Expect(createdService.Status.Status).To(Equal("Healthy"))
			}, timeout, interval).Should(Succeed())
		})

		AfterEach(func() {
			role := os.Getenv("ROLE")
			if role != ControllerRole {
				Skip("Skipping test because ROLE is not 'controller'")
			}

			createdService := &infrastructurev1alpha1.Service{}
			Expect(k8sClient.Get(ctx, serviceLookupKey, createdService)).To(Succeed())
			Expect(k8sClient.Delete(ctx, createdService)).To(Succeed())

			By("Waiting for Service to be deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, serviceLookupKey, createdService)
				if err != nil {
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(createdService.DeletionTimestamp.IsZero()).To(BeFalse())
				}
			}, timeout, interval).Should(Succeed())

			// Owned resources are not automatically deleted in test env. Clean up manually
			cert := &certmanagerv1.Certificate{}
			certLookupKey := types.NamespacedName{Name: StaticServiceName, Namespace: Namespace}
			By("Deleting Certificate")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, certLookupKey, cert)
				if err != nil {
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(k8sClient.Delete(ctx, cert)).To(Succeed())
				}
			}, timeout, interval).Should(Succeed())

			// Owned resources are not automatically deleted in test env. Clean up manually
			appset := &argoprojv1alpha1.ApplicationSet{}
			appsetLookupKey := types.NamespacedName{Name: StaticServiceName, Namespace: Namespace}
			By("Deleting ApplicationSet")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, appsetLookupKey, appset)
				if err != nil {
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(k8sClient.Delete(ctx, appset)).To(Succeed())
				}
			}, timeout, interval).Should(Succeed())
		})

		Context("When Creating a Service", func() {
			It("Should set status to Healthy, and Create a certificate", func() {
				By("Checking that Service is healthy")

				createdService := &infrastructurev1alpha1.Service{}
				Expect(k8sClient.Get(ctx, serviceLookupKey, createdService)).To(Succeed())
				Expect(createdService.Status.Status).To(Equal("Healthy"))

				By("Checking that Service Spec was set correctly")
				Expect(createdService.Spec.Name).To(Equal(StaticServiceName))
				Expect(createdService.Spec.Domain).To(Equal(StaticServiceName))
				Expect(createdService.Spec.OriginType).To(Equal("static"))
				Expect(createdService.Spec.StaticOrigins).To(HaveLen(1))
				Expect(createdService.Spec.StaticOrigins[0].Upstream).To(Equal("randomupstream.com"))
				Expect(createdService.Spec.StaticOrigins[0].Port).To(Equal(443))
				Expect(createdService.Spec.StaticOrigins[0].HostHeader).To(Equal("randomupstream.com"))
				Expect(createdService.Spec.StaticOrigins[0].Scheme).To(Equal("Https"))

				By("Checking that Certificate was created")
				cert := &certmanagerv1.Certificate{}
				certLookupKey := types.NamespacedName{Name: StaticServiceName, Namespace: Namespace}

				Expect(k8sClient.Get(ctx, certLookupKey, cert)).To(Succeed())
				Expect(cert.Spec.DNSNames).To(ContainElement(StaticServiceName))
			})
		})
	})
})
