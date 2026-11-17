package v1alpha1

import (
	"time"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/controller"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Shared infrastructure admission", func() {
	It("continues accepting and persisting legacy name-only local zone references", func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "legacy-customer"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed())
		})
		project := &api.Project{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: ns.Name}}
		Expect(k8sClient.Create(ctx, project)).To(Succeed())
		zone := &api.Zone{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: "legacy"},
			Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: project.Name},
				Zone: "legacy.admission.example.test", Email: "noc.example.test"}}
		Expect(k8sClient.Create(ctx, zone)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, zone))).To(Succeed())
		})
		endpoint := &api.DNSEndpoint{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: "legacy"},
			Spec: api.DNSEndpointSpec{Project: &corev1.LocalObjectReference{Name: project.Name},
				Zone: &corev1.ObjectReference{Name: zone.Name}, DNSName: "www.legacy.admission.example.test",
				RecordType: "A", RoutingPolicy: "Simple", Targets: []string{"192.0.2.1"}}}
		Eventually(func() error { return k8sClient.Create(ctx, endpoint) }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, endpoint))).To(Succeed())
		})
		persisted := &api.DNSEndpoint{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(endpoint), persisted)).To(Succeed())
		Expect(persisted.Spec.Zone.Name).To(Equal(zone.Name))
		Expect(persisted.Spec.Zone.Namespace).To(BeEmpty())
		Expect(persisted.ZoneKey()).To(Equal(client.ObjectKeyFromObject(zone)))
	})

	It("persists remote zone references and blocks revocation and deletion until services are drained", func() {
		for _, namespace := range []string{"shared-platform", "shared-customer"} {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed())
			})
			project := &api.Project{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: namespace}}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
		}
		zone := &api.Zone{ObjectMeta: metav1.ObjectMeta{Namespace: "shared-platform", Name: "cdn"},
			Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "shared-platform"},
				Zone: "shared.admission.example.test", Email: "noc.example.test",
				Sharing: &api.ResourceSharing{Projects: []api.ProjectReference{{Namespace: "shared-customer", Name: "shared-customer"}}}}}
		Expect(k8sClient.Create(ctx, zone)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, zone))).To(Succeed())
		})
		service := &api.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shared-customer", Name: "cdn"},
			Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "shared-customer"},
				Domain: "allocated.shared.admission.example.test", OriginType: "static",
				StaticOrigins: []api.StaticOriginSpec{{Upstream: "origin.example.test"}},
				RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cdn"}}}}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		routing := &controller.ServiceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(service)}
		drain := func() {
			endpoints := &api.DNSEndpointList{}
			Expect(k8sClient.List(ctx, endpoints, client.InNamespace(service.Namespace))).To(Succeed())
			for i := range endpoints.Items {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &endpoints.Items[i]))).To(Succeed())
			}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, service))).To(Succeed())
			_, err := routing.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
		}
		DeferCleanup(drain)
		Eventually(func() error {
			_, err := routing.Reconcile(ctx, request)
			return err
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		endpoints := &api.DNSEndpointList{}
		Expect(k8sClient.List(ctx, endpoints, client.InNamespace(service.Namespace))).To(Succeed())
		Expect(endpoints.Items).To(HaveLen(2))
		for i := range endpoints.Items {
			Expect(endpoints.Items[i].Spec.Zone.Namespace).To(Equal(zone.Namespace))
			Expect(endpoints.Items[i].Spec.Zone.Name).To(Equal(zone.Name))
		}
		revoked := zone.DeepCopy()
		revoked.Spec.Sharing = nil
		Expect(k8sClient.Update(ctx, revoked)).To(MatchError(ContainSubstring("cannot revoke sharing")))
		Expect(k8sClient.Delete(ctx, zone)).To(MatchError(ContainSubstring("still in use")))
		drain()
		Expect(k8sClient.Update(ctx, revoked)).To(Succeed())
	})
})
