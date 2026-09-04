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
	"encoding/json"
	"time"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

var _ = Describe("HealthCheckProfile Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName = "test-healthcheckprofile"
			namespace    = "default"
			timeout      = time.Second * 5
			interval     = time.Millisecond * 500
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		throwerOptions := builder.ThrowerOptions{
			ThrowerChartName:       ThrowerChartName,
			ThrowerChartVersion:    ThrowerChartVersion,
			ThrowerChartRepository: ThrowerChartRepository,
			TargetNamespace:        InfrastructureTargetNamespace,
			ApplicationSetProject:  InfrastructureApplicationSetProject,
		}

		newHealthCheckProfile := func(name string) *infrastructurev1alpha1.HealthCheckProfile {
			return &infrastructurev1alpha1.HealthCheckProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: infrastructurev1alpha1.HealthCheckProfileSpec{
					Probes: []infrastructurev1alpha1.HealthCheckProbeSpec{
						{
							Name:     "tcp-default",
							Type:     infrastructurev1alpha1.HealthCheckProbeTypeTCP,
							Interval: metav1.Duration{Duration: 30 * time.Second},
							Timeout:  metav1.Duration{Duration: 5 * time.Second},
							TCP: &infrastructurev1alpha1.TCPHealthCheckProbeSpec{
								Port: 80,
							},
						},
						{
							Name:     "http-ready",
							Type:     infrastructurev1alpha1.HealthCheckProbeTypeHTTP,
							Interval: metav1.Duration{Duration: 30 * time.Second},
							Timeout:  metav1.Duration{Duration: 5 * time.Second},
							HTTP: &infrastructurev1alpha1.HTTPHealthCheckProbeSpec{
								Port: 80,
								Path: "/healthz",
							},
						},
						{
							Name:     "https-ready",
							Type:     infrastructurev1alpha1.HealthCheckProbeTypeHTTPS,
							Interval: metav1.Duration{Duration: 30 * time.Second},
							Timeout:  metav1.Duration{Duration: 5 * time.Second},
							HTTPS: &infrastructurev1alpha1.HTTPHealthCheckProbeSpec{
								Port: 443,
								Path: "/healthz",
							},
						},
						{
							Name: "assumed-healthy",
							Type: infrastructurev1alpha1.HealthCheckProbeTypeASSUME,
							Assume: &infrastructurev1alpha1.AssumeHealthCheckProbeSpec{
								Status: infrastructurev1alpha1.AssumedHealthStatusHealthy,
							},
						},
					},
				},
			}
		}

		deleteIfExists := func(obj client.Object) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
			if apierrors.IsNotFound(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
		}

		AfterEach(func() {
			deleteIfExists(&argoprojv1alpha1.ApplicationSet{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: namespace}})
			deleteIfExists(&infrastructurev1alpha1.HealthCheckProfile{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: namespace}})
		})

		It("accepts multiple probe types on one profile", func() {
			profile := newHealthCheckProfile(resourceName)
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			createdProfile := &infrastructurev1alpha1.HealthCheckProfile{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, createdProfile)).To(Succeed())
			Expect(createdProfile.Spec.Probes).To(HaveLen(4))
			Expect(createdProfile.Spec.Probes[0].TCP.Port).To(Equal(int32(80)))
			Expect(createdProfile.Spec.Probes[1].HTTP.Path).To(Equal("/healthz"))
			Expect(createdProfile.Spec.Probes[2].HTTPS.Port).To(Equal(int32(443)))
			Expect(createdProfile.Spec.Probes[3].Assume.Status).To(Equal(infrastructurev1alpha1.AssumedHealthStatusHealthy))
		})

		It("rejects a probe whose type does not match its config block", func() {
			profile := newHealthCheckProfile(resourceName)
			profile.Spec.Probes = []infrastructurev1alpha1.HealthCheckProbeSpec{
				{
					Name: "tcp-with-http-config",
					Type: infrastructurev1alpha1.HealthCheckProbeTypeTCP,
					HTTP: &infrastructurev1alpha1.HTTPHealthCheckProbeSpec{Port: 80},
				},
			}

			err := k8sClient.Create(ctx, profile)
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid HealthCheckProfile, got %v", err)
		})

		It("progresses to healthy and creates an ApplicationSet", func() {
			profile := newHealthCheckProfile(resourceName)
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			controllerReconciler := &HealthCheckProfileReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				ThrowerOptions: throwerOptions,
			}

			reconcileRequest := reconcile.Request{
				NamespacedName: typeNamespacedName,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			createdProfile := &infrastructurev1alpha1.HealthCheckProfile{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, createdProfile)).To(Succeed())
			Expect(createdProfile.Status.Status).To(Equal(HealthStatusProgressing))

			_, err = controllerReconciler.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			applicationSet := &argoprojv1alpha1.ApplicationSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, applicationSet)).To(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(applicationSet.Annotations).To(HaveKey(builder.ValuesHashAnnotation))

			values := struct {
				Resources []infrastructurev1alpha1.HealthCheckProfile `json:"resources"`
			}{}
			Expect(json.Unmarshal(applicationSet.Spec.Template.Spec.Sources[0].Helm.ValuesObject.Raw, &values)).To(Succeed())
			Expect(values.Resources).To(HaveLen(1))
			Expect(values.Resources[0].Name).To(Equal(resourceName))
			Expect(values.Resources[0].Namespace).To(BeEmpty())
			Expect(values.Resources[0].Spec.Probes).To(HaveLen(4))

			_, err = controllerReconciler.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, createdProfile)).To(Succeed())
			Expect(createdProfile.Status.Status).To(Equal(HealthStatusHealthy))
		})
	})
})
