package controller

import (
	"context"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

var _ = Describe("Service Routing Controller", func() {
	const resourceName = "routing-test-service"

	ctx := context.Background()
	serviceKey := types.NamespacedName{Name: resourceName, Namespace: "default"}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"edgecdnx.com/routing-instance": "edgecdnx"}}

	BeforeEach(func() {
		service := &infrastructurev1alpha1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
			Spec: infrastructurev1alpha1.ServiceSpec{
				Domain:        "routing-test.edgecdnx.com",
				HostAliases:   []infrastructurev1alpha1.HostAliasSpec{{Name: "routing-alias.example.com"}},
				RouteSelector: selector,
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
	})

	AfterEach(func() {
		for _, dnsName := range []string{"routing-test.edgecdnx.com", "routing-alias.example.com"} {
			for _, recordType := range []string{"A", "AAAA"} {
				dnsEndpoint := &infrastructurev1alpha1.DNSEndpoint{}
				key := types.NamespacedName{
					Name:      serviceDNSEndpointName(resourceName, dnsName, recordType),
					Namespace: "default",
				}
				if err := k8sClient.Get(ctx, key, dnsEndpoint); err == nil {
					Expect(k8sClient.Delete(ctx, dnsEndpoint)).To(Succeed())
				}
			}
		}

		service := &infrastructurev1alpha1.Service{}
		if err := k8sClient.Get(ctx, serviceKey, service); err == nil {
			Expect(k8sClient.Delete(ctx, service)).To(Succeed())
		}
	})

	It("creates and maintains A and AAAA endpoints for the domain and host aliases", func() {
		reconciler := &ServiceRoutingReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: serviceKey})
		Expect(err).NotTo(HaveOccurred())
		service := &infrastructurev1alpha1.Service{}
		Expect(k8sClient.Get(ctx, serviceKey, service)).To(Succeed())

		for _, dnsName := range []string{"routing-test.edgecdnx.com", "routing-alias.example.com"} {
			for _, recordType := range []string{"A", "AAAA"} {
				name := serviceDNSEndpointName(resourceName, dnsName, recordType)
				dnsEndpoint := &infrastructurev1alpha1.DNSEndpoint{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, dnsEndpoint)).To(Succeed())
				Expect(dnsEndpoint.Spec.DNSName).To(Equal(dnsName))
				Expect(dnsEndpoint.Spec.RecordType).To(Equal(recordType))
				Expect(dnsEndpoint.Spec.RoutingPolicy).To(Equal(builder.DNSRoutingPolicyGeolocation))
				Expect(dnsEndpoint.Spec.RouteSelector).To(Equal(selector))
				Expect(dnsEndpoint.Spec.Targets).To(BeNil())
				Expect(dnsEndpoint.Annotations).To(HaveKey(builder.ValuesHashAnnotation))
				Expect(metav1.IsControlledBy(dnsEndpoint, service)).To(BeTrue())
			}
		}

		primaryAKey := types.NamespacedName{
			Name:      serviceDNSEndpointName(resourceName, "routing-test.edgecdnx.com", "A"),
			Namespace: "default",
		}
		primaryA := &infrastructurev1alpha1.DNSEndpoint{}
		Expect(k8sClient.Get(ctx, primaryAKey, primaryA)).To(Succeed())
		primaryA.Spec.RoutingPolicy = "Simple"
		primaryA.Annotations[builder.ValuesHashAnnotation] = "drifted"
		Expect(k8sClient.Update(ctx, primaryA)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: serviceKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, primaryAKey, primaryA)).To(Succeed())
		Expect(primaryA.Spec.RoutingPolicy).To(Equal(builder.DNSRoutingPolicyGeolocation))
		Expect(primaryA.Annotations[builder.ValuesHashAnnotation]).NotTo(Equal("drifted"))

		Expect(k8sClient.Get(ctx, serviceKey, service)).To(Succeed())
		service.Spec.HostAliases = nil
		Expect(k8sClient.Update(ctx, service)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: serviceKey})
		Expect(err).NotTo(HaveOccurred())

		for _, recordType := range []string{"A", "AAAA"} {
			aliasEndpoint := &infrastructurev1alpha1.DNSEndpoint{}
			aliasKey := types.NamespacedName{
				Name:      serviceDNSEndpointName(resourceName, "routing-alias.example.com", recordType),
				Namespace: "default",
			}
			Expect(k8sClient.Get(ctx, aliasKey, aliasEndpoint)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		}
	})
})
