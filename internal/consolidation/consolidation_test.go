package consolidation

import (
	"context"
	"slices"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConsolidation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Consolidation Suite")
}

var _ = Describe("Consolidation", func() {

	var PrefixesV4 []infrastructurev1alpha1.V4PrefixSpec = make([]infrastructurev1alpha1.V4PrefixSpec, 0)
	var PrefixesV6 []infrastructurev1alpha1.V6PrefixSpec = make([]infrastructurev1alpha1.V6PrefixSpec, 0)

	It("ConsolidateV4 - Non Overlapping", func() {
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "192.168.0.0", Size: 24})
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "192.168.2.0", Size: 24})
		consolidated, err := ConsolidateV4(context.TODO(), PrefixesV4)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(2))
	})

	It("ConsolidateV6 - Non Overlapping", func() {
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2001:0000::", Size: 32})
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2001:0002::", Size: 32})
		consolidated, err := ConsolidateV6(context.TODO(), PrefixesV6)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(2))
	})

	It("ConsolidateV4 - Prefixes joined", func() {
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "192.168.1.0", Size: 24})
		consolidated, err := ConsolidateV4(context.TODO(), PrefixesV4)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(2))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V4PrefixSpec) bool {
			return p.Address == "192.168.0.0" && p.Size == 23
		})).To(BeTrue())
	})

	It("ConsolidateV6 - Prefixes joined", func() {
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2001:0001::", Size: 32})
		consolidated, err := ConsolidateV6(context.TODO(), PrefixesV6)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(2))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V6PrefixSpec) bool {
			return p.Address == "2001::" && p.Size == 31
		})).To(BeTrue())
	})

	It("ConsolidateV4 - Supernet", func() {
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "192.168.0.0", Size: 16})
		consolidated, err := ConsolidateV4(context.TODO(), PrefixesV4)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(1))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V4PrefixSpec) bool {
			return p.Address == "192.168.0.0" && p.Size == 16
		})).To(BeTrue())
	})

	It("ConsolidateV6 - Supernet", func() {
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2001::", Size: 24})
		consolidated, err := ConsolidateV6(context.TODO(), PrefixesV6)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(1))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V6PrefixSpec) bool {
			return p.Address == "2001::" && p.Size == 24
		})).To(BeTrue())
	})

	It("ConsolidateV4 - Multisupernet", func() {
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "192.168.0.0", Size: 8})
		consolidated, err := ConsolidateV4(context.TODO(), PrefixesV4)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(1))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V4PrefixSpec) bool {
			return p.Address == "192.168.0.0" && p.Size == 8
		})).To(BeTrue())
	})

	It("ConsolidateV6 - Multisupernet", func() {
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2001::", Size: 16})
		consolidated, err := ConsolidateV6(context.TODO(), PrefixesV6)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(1))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V6PrefixSpec) bool {
			return p.Address == "2001::" && p.Size == 16
		})).To(BeTrue())
	})

	It("ConsolidateV4 - Multijoin", func() {
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "10.0.0.0", Size: 10})
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "10.64.0.0", Size: 10})
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "10.128.0.0", Size: 10})
		PrefixesV4 = append(PrefixesV4, infrastructurev1alpha1.V4PrefixSpec{Address: "10.192.0.0", Size: 10})

		consolidated, err := ConsolidateV4(context.TODO(), PrefixesV4)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(2))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V4PrefixSpec) bool {
			return p.Address == "192.168.0.0" && p.Size == 8
		})).To(BeTrue())
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V4PrefixSpec) bool {
			return p.Address == "10.0.0.0" && p.Size == 8
		})).To(BeTrue())
	})

	It("ConsolidateV6 - Multijoin", func() {
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2002:0000::", Size: 18})
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2002:4000::", Size: 18})
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2002:8000::", Size: 18})
		PrefixesV6 = append(PrefixesV6, infrastructurev1alpha1.V6PrefixSpec{Address: "2002:c000::", Size: 18})

		consolidated, err := ConsolidateV6(context.TODO(), PrefixesV6)

		Expect(err).To(BeNil())
		Expect(len(consolidated)).To(Equal(2))
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V6PrefixSpec) bool {
			return p.Address == "2001::" && p.Size == 16
		})).To(BeTrue())
		Expect(slices.ContainsFunc(consolidated, func(p infrastructurev1alpha1.V6PrefixSpec) bool {
			return p.Address == "2002::" && p.Size == 16
		})).To(BeTrue())
	})

})
