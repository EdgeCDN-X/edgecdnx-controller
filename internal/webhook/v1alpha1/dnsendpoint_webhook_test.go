/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
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
			obj.Spec.DNSName = "www.example.com"
			obj.Spec.RecordType = "A"
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"192.0.2.1"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
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
			obj.Spec.DNSName = "www.example.com"
			obj.Spec.RecordType = "A"
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"192.0.2.1"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
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

		It("rejects a duplicate owner and record type regardless of DNS name case or trailing dot", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "WWW.EXAMPLE.COM"
			obj.Spec.RecordType = "A"
			obj.Spec.RoutingPolicy = "RoundRobin"
			obj.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"region": "us-west"}}
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"192.0.2.2"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			existing := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "existing-record"},
				Spec: infrastructurev1alpha1.DNSEndpointSpec{
					Zone: &corev1.LocalObjectReference{Name: "example-zone"}, DNSName: "www.example.com.",
					RecordType: "A", RoutingPolicy: "Geolocation",
					RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"region": "us-east"}},
				},
			}
			Expect(validator.Client.Create(context.Background(), existing)).To(Succeed())

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("already exists")))
		})

		It("rejects a dynamic endpoint when a Simple endpoint already has the same owner and type", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "www.example.com"
			obj.Spec.RecordType = "A"
			obj.Spec.RoutingPolicy = "Weighted"
			obj.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"region": "us-west"}}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			existing := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "simple-record"},
				Spec: infrastructurev1alpha1.DNSEndpointSpec{
					Zone: &corev1.LocalObjectReference{Name: "example-zone"}, DNSName: "www.example.com",
					RecordType: "A", RoutingPolicy: "Simple", Targets: []string{"192.0.2.1"},
				},
			}
			Expect(validator.Client.Create(context.Background(), existing)).To(Succeed())

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("already exists")))
		})

		It("rejects a dynamic address endpoint when a Simple CNAME exists at the owner", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "www.example.com"
			obj.Spec.RecordType = "A"
			obj.Spec.RoutingPolicy = "Geolocation"
			obj.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"region": "us-west"}}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			existing := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "simple-alias"},
				Spec: infrastructurev1alpha1.DNSEndpointSpec{
					Zone: &corev1.LocalObjectReference{Name: "example-zone"}, DNSName: "www.example.com",
					RecordType: "CNAME", RoutingPolicy: "Simple", Targets: []string{"edge.example.net."},
				},
			}
			Expect(validator.Client.Create(context.Background(), existing)).To(Succeed())

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("CNAME record")))
		})

		It("rejects a CNAME when another record type already exists at its owner", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "www.example.com"
			obj.Spec.RecordType = "CNAME"
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"edge.example.net."}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			existing := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "existing-record"},
				Spec: infrastructurev1alpha1.DNSEndpointSpec{
					Zone: &corev1.LocalObjectReference{Name: "example-zone"}, DNSName: "www.example.com",
					RecordType: "A", RoutingPolicy: "Simple", Targets: []string{"192.0.2.1"},
				},
			}
			Expect(validator.Client.Create(context.Background(), existing)).To(Succeed())

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("CNAME record")))
		})

		It("rejects MX targets that are CNAMEs in another zone", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "example.com"
			obj.Spec.RecordType = "MX"
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"10 mail.other.net."}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			otherZone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "other-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "other.net"},
			}
			Expect(validator.Client.Create(context.Background(), otherZone)).To(Succeed())
			cname := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "mail-alias"},
				Spec: infrastructurev1alpha1.DNSEndpointSpec{
					Zone: &corev1.LocalObjectReference{Name: "other-zone"}, DNSName: "mail.other.net",
					RecordType: "CNAME", RoutingPolicy: "Simple", Targets: []string{"edge.example.net."},
				},
			}
			Expect(validator.Client.Create(context.Background(), cname)).To(Succeed())

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("MX target")))
		})

		It("rejects creating a CNAME for an existing MX target", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "mail.example.com"
			obj.Spec.RecordType = "CNAME"
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"edge.example.net."}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			existing := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "mail-exchange"},
				Spec: infrastructurev1alpha1.DNSEndpointSpec{
					Zone: &corev1.LocalObjectReference{Name: "example-zone"}, DNSName: "example.com",
					RecordType: "MX", RoutingPolicy: "Simple", Targets: []string{"10 mail.example.com."},
				},
			}
			Expect(validator.Client.Create(context.Background(), existing)).To(Succeed())

			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("MX target")))
		})

		It("rejects malformed address record data and out-of-range TTLs", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "www.example.com"
			obj.Spec.RecordType = "A"
			obj.Spec.RecordTTL = 300
			obj.Spec.Targets = []string{"not-an-ip-address"}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())
			_, err := validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("invalid target")))

			obj.Spec.Targets = []string{"192.0.2.1"}
			obj.Spec.RecordTTL = 1 << 31
			_, err = validator.ValidateCreate(context.Background(), obj)
			Expect(err).To(MatchError(ContainSubstring("spec.recordTTL")))
		})

		It("rejects IPv4 and IPv6 literals as CNAME targets", func() {
			for _, target := range []string{"192.0.2.1", "2001:db8::1"} {
				endpoint := &infrastructurev1alpha1.DNSEndpoint{
					Spec: infrastructurev1alpha1.DNSEndpointSpec{
						DNSName: "alias.example.com", RecordType: "CNAME", RoutingPolicy: "Simple",
						RecordTTL: 300, Targets: []string{target},
					},
				}
				err := validateDNSRecordTargets(endpoint)
				Expect(err).To(MatchError(ContainSubstring("not an IP address")), target)
			}
		})

		It("allows only A and AAAA record types with non-Simple routing", func() {
			obj.Namespace = "default"
			obj.Spec.Zone = &corev1.LocalObjectReference{Name: "example-zone"}
			obj.Spec.DNSName = "example.com"
			obj.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"region": "us-east"}}
			zone := &infrastructurev1alpha1.Zone{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-zone"},
				Spec:       infrastructurev1alpha1.ZoneSpec{Zone: "example.com"},
			}
			Expect(validator.Client.Create(context.Background(), zone)).To(Succeed())

			for _, policy := range []string{"Geolocation", "Weighted", "RoundRobin", "Failover"} {
				obj.Spec.RoutingPolicy = policy
				for _, recordType := range []string{"CNAME", "TXT", "MX", "SRV", "NS"} {
					obj.Spec.RecordType = recordType
					_, err := validator.ValidateCreate(context.Background(), obj)
					Expect(err).To(MatchError(ContainSubstring("not supported with routingPolicy")), "%s with %s", policy, recordType)
				}
				for _, recordType := range []string{"A", "AAAA"} {
					obj.Spec.RecordType = recordType
					_, err := validator.ValidateCreate(context.Background(), obj)
					Expect(err).NotTo(HaveOccurred(), "%s with %s", policy, recordType)
				}
			}
		})
	})

})
