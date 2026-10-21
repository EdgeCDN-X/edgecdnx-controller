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
	"os"
	"time"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PrefixList Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			prefixListName = "test-prefixlist"
			namespace      = "default"
			timeout        = time.Second * 5
			interval       = time.Millisecond * 500
		)

		prefixListLookupKey := types.NamespacedName{Name: prefixListName, Namespace: namespace}
		secondPrefixListLookupKey := types.NamespacedName{Name: "test-prefixlist-second", Namespace: namespace}

		BeforeEach(func() {
			if os.Getenv("ROLE") != "controller" {
				Skip("Skipping PrefixList controller tests as the ROLE is not set to 'controller'")
			}

			prefixList := &infrastructurev1alpha1.PrefixList{
				ObjectMeta: metav1.ObjectMeta{Name: prefixListName, Namespace: namespace},
				Spec: infrastructurev1alpha1.PrefixListSpec{
					Project:     &corev1.LocalObjectReference{Name: namespace},
					Source:      "Static",
					Destination: "location-a",
					Prefix: infrastructurev1alpha1.PrefixSpec{
						V4: []infrastructurev1alpha1.V4PrefixSpec{{Address: "192.168.0.0", Size: 25}},
						V6: []infrastructurev1alpha1.V6PrefixSpec{{Address: "2001:0db8:85a3::", Size: 64}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, prefixList)).To(Succeed())

			secondPrefixList := &infrastructurev1alpha1.PrefixList{
				ObjectMeta: metav1.ObjectMeta{Name: secondPrefixListLookupKey.Name, Namespace: namespace},
				Spec: infrastructurev1alpha1.PrefixListSpec{
					Project:     &corev1.LocalObjectReference{Name: namespace},
					Source:      "Bgp",
					Destination: "location-a",
					Prefix: infrastructurev1alpha1.PrefixSpec{
						V4: []infrastructurev1alpha1.V4PrefixSpec{{Address: "198.51.100.0", Size: 24}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, secondPrefixList)).To(Succeed())
		})

		AfterEach(func() {
			if os.Getenv("ROLE") != "controller" {
				Skip("Skipping PrefixList controller tests as the ROLE is not set to 'controller'")
			}

			for _, key := range []types.NamespacedName{prefixListLookupKey, secondPrefixListLookupKey} {
				prefixList := &infrastructurev1alpha1.PrefixList{}
				Expect(k8sClient.Get(ctx, key, prefixList)).To(Succeed())
				Expect(k8sClient.Delete(ctx, prefixList)).To(Succeed())
			}
		})

		It("keeps same-destination PrefixLists separate and healthy", func() {
			Eventually(func(g Gomega) {
				for _, key := range []types.NamespacedName{prefixListLookupKey, secondPrefixListLookupKey} {
					prefixList := &infrastructurev1alpha1.PrefixList{}
					g.Expect(k8sClient.Get(ctx, key, prefixList)).To(Succeed())
					g.Expect(prefixList.Status.Status).To(Equal(HealthStatusHealthy))
				}
			}, timeout, interval).Should(Succeed())

			Consistently(func(g Gomega) {
				prefixLists := &infrastructurev1alpha1.PrefixListList{}
				g.Expect(k8sClient.List(ctx, prefixLists, client.InNamespace(namespace))).To(Succeed())
				g.Expect(prefixLists.Items).To(HaveLen(2))

				applicationSets := &argoprojv1alpha1.ApplicationSetList{}
				g.Expect(k8sClient.List(ctx, applicationSets, client.InNamespace(namespace))).To(Succeed())
				for _, applicationSet := range applicationSets.Items {
					for _, ownerReference := range applicationSet.OwnerReferences {
						g.Expect(ownerReference.Kind).NotTo(Equal("PrefixList"))
					}
				}
			}, time.Second*2, interval).Should(Succeed())
		})
	})
})
