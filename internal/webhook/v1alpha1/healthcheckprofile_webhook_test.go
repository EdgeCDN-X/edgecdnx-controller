/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package v1alpha1

import (
	"context"
	"time"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("HealthCheckProfile Webhook", func() {
	var (
		validator HealthCheckProfileCustomValidator
		profile   *infrastructurev1alpha1.HealthCheckProfile
	)

	BeforeEach(func() {
		profile = &infrastructurev1alpha1.HealthCheckProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "web"},
			Spec: infrastructurev1alpha1.HealthCheckProfileSpec{
				Probes: []infrastructurev1alpha1.HealthCheckProbeSpec{{
					Name:     "tcp",
					Interval: metav1.Duration{Duration: 30 * time.Second},
					Timeout:  metav1.Duration{Duration: 5 * time.Second},
				}},
			},
		}
	})

	It("admits interval at 5s when timeout is shorter", func() {
		profile.Spec.Probes[0].Interval.Duration = 5 * time.Second
		profile.Spec.Probes[0].Timeout.Duration = 4 * time.Second

		_, err := validator.ValidateCreate(context.Background(), profile)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects intervals shorter than 5s", func() {
		profile.Spec.Probes[0].Interval.Duration = 4999 * time.Millisecond

		_, err := validator.ValidateCreate(context.Background(), profile)
		Expect(err).To(MatchError(`probe "tcp" interval must be at least 5s`))
	})

	It("rejects a timeout equal to or greater than the interval on create and update", func() {
		for _, timeout := range []time.Duration{30 * time.Second, 31 * time.Second} {
			profile.Spec.Probes[0].Timeout.Duration = timeout

			_, createErr := validator.ValidateCreate(context.Background(), profile)
			Expect(createErr).To(MatchError(`probe "tcp" timeout must be greater than 0 and less than its interval`))

			_, updateErr := validator.ValidateUpdate(context.Background(), profile, profile)
			Expect(updateErr).To(MatchError(`probe "tcp" timeout must be greater than 0 and less than its interval`))
		}
	})

	It("rejects non-positive timeouts", func() {
		profile.Spec.Probes[0].Timeout.Duration = 0

		_, err := validator.ValidateCreate(context.Background(), profile)
		Expect(err).To(MatchError(`probe "tcp" timeout must be greater than 0 and less than its interval`))
	})
})
