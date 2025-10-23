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
	"fmt"
	"os"
	"time"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("PrefixList Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			PrefixListName      = "test-prefixlist"
			Namespace           = "default"
			timeout             = time.Second * 5
			interval            = time.Millisecond * 500
			ControllerRole      = "controller"
			DestinationLocation = "location-a"
		)

		prefixListLookupKey := types.NamespacedName{Name: PrefixListName, Namespace: Namespace}
		prefixlist := &infrastructurev1alpha1.PrefixList{
			ObjectMeta: metav1.ObjectMeta{
				Name:      PrefixListName,
				Namespace: Namespace,
			},
			Spec: infrastructurev1alpha1.PrefixListSpec{
				Source:      "Static",
				Destination: DestinationLocation,
				Prefix: infrastructurev1alpha1.PrefixSpec{
					V4: []infrastructurev1alpha1.V4PrefixSpec{
						{
							Address: "192.168.0.0",
							Size:    25,
						},
					},
					V6: []infrastructurev1alpha1.V6PrefixSpec{
						{
							Address: "2001:0db8:85a3::",
							Size:    64,
						},
					},
				},
			},
		}

		BeforeEach(func() {
			role := os.Getenv("ROLE")
			if role != ControllerRole {
				Skip("Skipping PrefixList controller tests as the ROLE is not set to 'controller'")
			}

			By("creating the custom resource for the Kind PrefixList")

			newPrefixList := &infrastructurev1alpha1.PrefixList{
				ObjectMeta: metav1.ObjectMeta{
					Name:      PrefixListName,
					Namespace: Namespace,
				},
				Spec: prefixlist.Spec,
			}

			Expect(k8sClient.Create(ctx, newPrefixList)).To(Succeed())

			prefixlistCreated := &infrastructurev1alpha1.PrefixList{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, prefixListLookupKey, prefixlistCreated)).To(Succeed())
				g.Expect(prefixlistCreated.Status.Status).To(Equal("Healthy"))
			}, timeout, interval).Should(Succeed())
		})

		AfterEach(func() {
			role := os.Getenv("ROLE")
			if role != ControllerRole {
				Skip("Skipping PrefixList controller tests as the ROLE is not set to 'controller'")
			}
			// Cleanup logic after each test, like removing the resource instance.
			resource := &infrastructurev1alpha1.PrefixList{}
			err := k8sClient.Get(ctx, prefixListLookupKey, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance PrefixList")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Deleting The Generated Prefixlist")
			generatedPrefixlist := &infrastructurev1alpha1.PrefixList{}
			generatedPrefixlistLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
			Expect(k8sClient.Get(ctx, generatedPrefixlistLookupKey, generatedPrefixlist)).To(Succeed())
			Expect(k8sClient.Delete(ctx, generatedPrefixlist)).To(Succeed())

			By("Deleting PrefixList ApplicationSet")
			applicationSet := &argoprojv1alpha1.ApplicationSet{}
			applicationSetLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
			Expect(k8sClient.Get(ctx, applicationSetLookupKey, applicationSet)).To(Succeed())
			Expect(k8sClient.Delete(ctx, applicationSet)).To(Succeed())
		})

		Context("When Creating PrefixList", func() {
			It("Should create the corresponding Generated PrefixList", func() {
				generatedPrefixlist := &infrastructurev1alpha1.PrefixList{}
				generatedPrefixlistLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, generatedPrefixlistLookupKey, generatedPrefixlist)).To(Succeed())
					g.Expect(generatedPrefixlist.Spec.Source).To(Equal("Controller"))
					g.Expect(generatedPrefixlist.Spec.Prefix.V4).To(HaveLen(1))
					g.Expect(generatedPrefixlist.Spec.Prefix.V6).To(HaveLen(1))
				}, timeout, interval).Should(Succeed())
			})

			It("Should create the corresponding ApplicationSet And should have the correct Hash", func() {
				applicationSet := &argoprojv1alpha1.ApplicationSet{}
				applicationSetLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, applicationSetLookupKey, applicationSet)).To(Succeed())
				}, timeout, interval).Should(Succeed())
			})

			It("Should have the correct hash on the applicationset", func() {
				applicationSet := &argoprojv1alpha1.ApplicationSet{}
				applicationSetLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Expect(k8sClient.Get(ctx, applicationSetLookupKey, applicationSet)).To(Succeed())

				generatedPrefixlist := &infrastructurev1alpha1.PrefixList{}
				generatedPrefixlistLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Expect(k8sClient.Get(ctx, generatedPrefixlistLookupKey, generatedPrefixlist)).To(Succeed())

				resource := &infrastructurev1alpha1.PrefixList{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "infrastructure.edgecdnx.com/v1alpha1",
						Kind:       "PrefixList",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: generatedPrefixlist.Name,
					},
					Spec: generatedPrefixlist.Spec,
				}

				prefixesHelmValues := struct {
					Resources []any `json:"resources"`
				}{
					Resources: []any{resource},
				}

				appsetBuilder, err := builder.AppsetBuilderFactory("PrefixList", generatedPrefixlist.Name, generatedPrefixlist.Namespace, builder.ThrowerOptions{
					ThrowerChartName:       ThrowerChartName,
					ThrowerChartVersion:    ThrowerChartVersion,
					ThrowerChartRepository: ThrowerChartRepository,
					TargetNamespace:        InfrastructureTargetNamespace,
					ApplicationSetProject:  InfrastructureApplicationSetProject,
				})
				Expect(err).NotTo(HaveOccurred())
				appsetBuilder.WithHelmValues(prefixesHelmValues)
				_, hash, err := appsetBuilder.Build()
				Expect(err).NotTo(HaveOccurred())

				Expect(applicationSet.ObjectMeta.Annotations).To(HaveKey(builder.ValuesHashAnnotation))
				Expect(applicationSet.ObjectMeta.Annotations[builder.ValuesHashAnnotation]).To(Equal(hash))
			})
		})

		Context("When Adding Prefixlist with different prefixes to consolidate", func() {
			It("Should update the Generated PrefixList with consolidated prefixes", func() {
				// Create another PrefixList with different prefixes but same destination
				anotherPrefixListLookupKey := types.NamespacedName{Name: "another-prefixlist", Namespace: Namespace}
				anotherPrefixList := &infrastructurev1alpha1.PrefixList{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "another-prefixlist",
						Namespace: Namespace,
					},
					Spec: infrastructurev1alpha1.PrefixListSpec{
						Source:      "Static",
						Destination: DestinationLocation,
						Prefix: infrastructurev1alpha1.PrefixSpec{
							V4: []infrastructurev1alpha1.V4PrefixSpec{
								{
									Address: "192.168.0.128",
									Size:    25,
								},
							},
							V6: []infrastructurev1alpha1.V6PrefixSpec{
								{
									Address: "2001:0db8:85a4::",
									Size:    64,
								},
							},
						},
					},
				}

				Expect(k8sClient.Create(ctx, anotherPrefixList)).To(Succeed())

				By("Adding a new Prefixlist with the same destination to consolidate prefixes")
				newPrefixListCreated := &infrastructurev1alpha1.PrefixList{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, anotherPrefixListLookupKey, newPrefixListCreated)).To(Succeed())
					g.Expect(newPrefixListCreated.Status.Status).To(Equal("Healthy"))
				}, timeout, interval).Should(Succeed())

				By("Verifying the Generated PrefixList has consolidated prefixes")
				generatedPrefixlist := &infrastructurev1alpha1.PrefixList{}
				generatedPrefixlistLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, generatedPrefixlistLookupKey, generatedPrefixlist)).To(Succeed())
					g.Expect(generatedPrefixlist.Spec.Source).To(Equal("Controller"))
					g.Expect(generatedPrefixlist.Spec.Prefix.V4).To(HaveLen(1))
					g.Expect(generatedPrefixlist.Spec.Prefix.V4[0].Address).To(Equal("192.168.0.0"))
					g.Expect(generatedPrefixlist.Spec.Prefix.V4[0].Size).To(Equal(24))
					g.Expect(generatedPrefixlist.Spec.Prefix.V6).To(HaveLen(2))
					g.Expect(generatedPrefixlist.ObjectMeta.OwnerReferences).To(HaveLen(2))
				}, timeout, interval).Should(Succeed())

				// Cleanup the another PrefixList
				Expect(k8sClient.Delete(ctx, newPrefixListCreated)).To(Succeed())
			})

			It("Should have the correct hash on the applicationset", func() {
				applicationSet := &argoprojv1alpha1.ApplicationSet{}
				applicationSetLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Expect(k8sClient.Get(ctx, applicationSetLookupKey, applicationSet)).To(Succeed())

				generatedPrefixlist := &infrastructurev1alpha1.PrefixList{}
				generatedPrefixlistLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-generated", DestinationLocation), Namespace: Namespace}
				Expect(k8sClient.Get(ctx, generatedPrefixlistLookupKey, generatedPrefixlist)).To(Succeed())

				resource := &infrastructurev1alpha1.PrefixList{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "infrastructure.edgecdnx.com/v1alpha1",
						Kind:       "PrefixList",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: generatedPrefixlist.Name,
					},
					Spec: generatedPrefixlist.Spec,
				}

				prefixesHelmValues := struct {
					Resources []any `json:"resources"`
				}{
					Resources: []any{resource},
				}

				appsetBuilder, err := builder.AppsetBuilderFactory("PrefixList", generatedPrefixlist.Name, generatedPrefixlist.Namespace, builder.ThrowerOptions{
					ThrowerChartName:       ThrowerChartName,
					ThrowerChartVersion:    ThrowerChartVersion,
					ThrowerChartRepository: ThrowerChartRepository,
					TargetNamespace:        InfrastructureTargetNamespace,
					ApplicationSetProject:  InfrastructureApplicationSetProject,
				})
				Expect(err).NotTo(HaveOccurred())
				appsetBuilder.WithHelmValues(prefixesHelmValues)
				_, hash, err := appsetBuilder.Build()
				Expect(err).NotTo(HaveOccurred())

				Expect(applicationSet.ObjectMeta.Annotations).To(HaveKey(builder.ValuesHashAnnotation))
				Expect(applicationSet.ObjectMeta.Annotations[builder.ValuesHashAnnotation]).To(Equal(hash))
			})
		})
	})
})
