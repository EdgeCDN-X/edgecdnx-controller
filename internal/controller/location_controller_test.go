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
	"time"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	throwable "github.com/EdgeCDN-X/edgecdnx-controller/internal/throwable"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

var _ = Describe("Location Reconciler", func() {
	Context("When Creating a Location resource", func() {
		It("Should set status to Healthy, and Create and ApplicationSet", func() {
			By("Creating a Location Resource")

			const (
				LocationName = "test-location"
				Namespace    = "default"
				timeout      = time.Second * 10
				duration     = time.Second * 10
				interval     = time.Millisecond * 250
			)

			location := &infrastructurev1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      LocationName,
					Namespace: Namespace,
				},
				Spec: infrastructurev1alpha1.LocationSpec{
					FallbackLocations: []string{"fallback1", "fallback2"},
					Nodes: []infrastructurev1alpha1.NodeSpec{
						{
							Name:   "node1",
							Ipv4:   "10.0.0.1",
							Ipv6:   "2001:db8::1",
							Caches: []string{"ssd"},
						},
					},
					GeoLookup: infrastructurev1alpha1.GeoLookupSpec{
						Weight: 10,
						Attributes: map[string]infrastructurev1alpha1.GeoLookupAttributeSpec{
							"country": {
								Weight: 5,
								Values: []infrastructurev1alpha1.GeoLookupAttributeValuesSpec{
									{Value: "US", Weight: 3},
									{Value: "CA", Weight: 2},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())

			locationLookupKey := types.NamespacedName{Name: LocationName, Namespace: Namespace}
			createdLocation := &infrastructurev1alpha1.Location{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, locationLookupKey, createdLocation)).To(Succeed())
				g.Expect(createdLocation.Status.Status).To(Equal("Healthy"))
			}, timeout, interval).Should(Succeed())

			// Run asserts
			By("Checking that the Location spec was set correctly")
			Expect(createdLocation.Spec.FallbackLocations).To(Equal([]string{"fallback1", "fallback2"}))
			Expect(createdLocation.Spec.Nodes).To(HaveLen(1))
			Expect(createdLocation.Spec.Nodes[0].Name).To(Equal("node1"))
			Expect(createdLocation.Spec.Nodes[0].Ipv4).To(Equal("10.0.0.1"))
			Expect(createdLocation.Spec.Nodes[0].Ipv6).To(Equal("2001:db8::1"))
			Expect(createdLocation.Spec.Nodes[0].Caches).To(Equal([]string{"ssd"}))
			Expect(createdLocation.Spec.GeoLookup.Weight).To(Equal(10))
			Expect(createdLocation.Spec.GeoLookup.Attributes).To(HaveKey("country"))
			Expect(createdLocation.Spec.GeoLookup.Attributes["country"].Weight).To(Equal(5))
			Expect(createdLocation.Spec.GeoLookup.Attributes["country"].Values).To(HaveLen(2))
			Expect(createdLocation.Spec.GeoLookup.Attributes["country"].Values[0].Value).To(Equal("US"))
			Expect(createdLocation.Spec.GeoLookup.Attributes["country"].Values[0].Weight).To(Equal(3))
			Expect(createdLocation.Spec.GeoLookup.Attributes["country"].Values[1].Value).To(Equal("CA"))
			Expect(createdLocation.Spec.GeoLookup.Attributes["country"].Values[1].Weight).To(Equal(2))

			By("Checking that the ApplicationSet has been created with the correct values")
			applicationSetLookupKey := types.NamespacedName{Name: LocationName, Namespace: Namespace}
			createdApplicationSet := &argoprojv1alpha1.ApplicationSet{}

			// Build resource for throwing
			resource := &infrastructurev1alpha1.Location{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "infrastructure.edgecdnx.com/v1alpha1",
					Kind:       "Location",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      createdLocation.Name,
					Namespace: InfrastructureTargetNamespace,
				},
				Spec: location.Spec,
			}

			// Build expected AppSet spec and hash
			locationHelmValues := throwable.ThrowerHelmValues{
				Resources: []any{resource},
			}

			// Get AppSet spec and hash
			_, _, hash, _ := locationHelmValues.GetAppSetSpec(
				throwable.AppsetSpecOptions{
					ChartRepository: ThrowerChartRepository,
					Chart:           ThrowerChartName,
					ChartVersion:    ThrowerChartVersion,
					AppsetNamespace: location.Namespace,
					Project:         InfrastructureApplicationSetProject,
					TargetNamespace: InfrastructureTargetNamespace,
					Name:            fmt.Sprintf(`{{ metadata.labels.edgecdnx.com/location }}-location-%s`, location.Name),
					// Roll out for routing
					LabelMatch: [][]metav1.LabelSelectorRequirement{
						{
							{
								Key:      "edgecdnx.com/routing",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"true", "yes"},
							},
						},
					},
				},
			)

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, applicationSetLookupKey, createdApplicationSet)).To(Succeed())
				createdApplicationSetHash := createdApplicationSet.Annotations[ValuesHashAnnotation]
				g.Expect(createdApplicationSetHash).To(Equal(hash))
			}, timeout, interval).Should(Succeed())
		})
	})
})
