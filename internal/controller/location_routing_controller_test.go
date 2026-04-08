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

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	monv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

var _ = Describe("Location Routing Reconciler", func() {
	const (
		locationName = "test-routing-location"
		namespace    = "default"
		timeout      = time.Second * 5
		interval     = time.Millisecond * 500
		routerRole   = "router"
	)

	locationLookupKey := types.NamespacedName{Name: locationName, Namespace: namespace}
	probeLookupKey := types.NamespacedName{Name: locationName, Namespace: namespace}

	location := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:      locationName,
			Namespace: namespace,
		},
		Spec: infrastructurev1alpha1.LocationSpec{
			NodeGroups: []infrastructurev1alpha1.NodeGroupSpec{
				{
					Name:   "cache",
					Flavor: "standard",
					Nodes: []infrastructurev1alpha1.NodeSpec{
						{
							Name: "node-1",
							Ipv4: "10.0.0.1",
							Ipv6: "2001:db8::1",
						},
						{
							Name: "node-2",
							Ipv4: "10.0.0.2",
						},
					},
					CacheConfig: infrastructurev1alpha1.CacheConfigSpec{
						Path:     "/var/cache/nginx",
						KeysZone: "cache_zone:10m",
						Inactive: "10m",
						MaxSize:  "1Gi",
					},
				},
			},
			GeoLookup: infrastructurev1alpha1.GeoLookupSpec{},
		},
	}

	BeforeEach(func() {
		if os.Getenv("ROLE") != routerRole {
			Skip("Skipping Location Routing Reconciler tests because ROLE is not set to 'router'")
		}

		newLocation := &infrastructurev1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      locationName,
				Namespace: namespace,
			},
			Spec: location.Spec,
		}

		Expect(k8sClient.Create(ctx, newLocation)).To(Succeed())
	})

	AfterEach(func() {
		if os.Getenv("ROLE") != routerRole {
			Skip("Skipping Location Routing Reconciler tests because ROLE is not set to 'router'")
		}

		createdLocation := &infrastructurev1alpha1.Location{}
		if err := k8sClient.Get(ctx, locationLookupKey, createdLocation); err == nil {
			Expect(k8sClient.Delete(ctx, createdLocation)).To(Succeed())
		}

		probe := &monv1.Probe{}
		if err := k8sClient.Get(ctx, probeLookupKey, probe); err == nil {
			Expect(k8sClient.Delete(ctx, probe)).To(Succeed())
		}
	})

	It("creates a Probe with ipv4 and ipv6 targets for node group nodes", func() {
		createdLocation := &infrastructurev1alpha1.Location{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, locationLookupKey, createdLocation)).To(Succeed())
			g.Expect(createdLocation.Status.Status).To(Equal(HealthStatusHealthy))
		}, timeout, interval).Should(Succeed())

		probe := &monv1.Probe{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, probeLookupKey, probe)).To(Succeed())
			g.Expect(probe.Spec.ProberSpec.URL).To(Equal(builder.DefaultLocationProbeProberURL))
			g.Expect(probe.Spec.Module).To(Equal(builder.LocationProbeModule))
			g.Expect(probe.Spec.Targets.StaticConfig).ToNot(BeNil())
			g.Expect(probe.Spec.Targets.StaticConfig.Targets).To(ConsistOf(
				"http://10.0.0.1/healthz",
				"http://[2001:db8::1]/healthz",
				"http://10.0.0.2/healthz",
			))
			g.Expect(probe.ObjectMeta.OwnerReferences).ToNot(BeEmpty())
			g.Expect(probe.ObjectMeta.OwnerReferences[0].UID).To(Equal(createdLocation.UID))
		}, timeout, interval).Should(Succeed())
	})

	It("updates Probe targets when node group nodes change", func() {
		existingLocation := &infrastructurev1alpha1.Location{}
		Expect(k8sClient.Get(ctx, locationLookupKey, existingLocation)).To(Succeed())

		existingLocation.Spec.NodeGroups[0].Nodes = []infrastructurev1alpha1.NodeSpec{
			{
				Name: "node-3",
				Ipv4: "10.0.0.3",
			},
			{
				Name: "node-4",
				Ipv6: "2001:db8::4",
			},
		}

		Expect(k8sClient.Update(ctx, existingLocation)).To(Succeed())

		probe := &monv1.Probe{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, probeLookupKey, probe)).To(Succeed())
			g.Expect(probe.Spec.Targets.StaticConfig).ToNot(BeNil())
			g.Expect(probe.Spec.Targets.StaticConfig.Targets).To(ConsistOf(
				"http://10.0.0.3/healthz",
				"http://[2001:db8::4]/healthz",
			))
		}, timeout, interval).Should(Succeed())
	})

	It("removes the Probe when a location has no probeable targets", func() {
		probe := &monv1.Probe{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, probeLookupKey, probe)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		existingLocation := &infrastructurev1alpha1.Location{}
		Expect(k8sClient.Get(ctx, locationLookupKey, existingLocation)).To(Succeed())
		existingLocation.Spec.NodeGroups[0].Nodes = nil
		Expect(k8sClient.Update(ctx, existingLocation)).To(Succeed())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, probeLookupKey, probe)
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}, timeout, interval).Should(Succeed())
	})
})
