// /*
// Copyright 2025.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package controller

import (
	"encoding/base64"
	"os"
	"time"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Service Controller", func() {
	Context("When reconciling a resource", func() {

		const (
			ServiceName    = "static.cdn.edgecdnx.com"
			Namespace      = "default"
			timeout        = time.Second * 10
			interval       = time.Millisecond * 500
			ControllerRole = "controller"

			tlsKey  = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFcFFJQkFBS0NBUUVBbTFXS25ySHM2QmlETjg0NTk4dHVYQnVWeHN0TElZa3BXWUJkSXdVNElZL21QN3hSCjVyNmZReTlpSnlKLzNWWU5nVTE2YmpyKytWMEsxUmxUZDkrRDgwRXNIbG5zOC9mSGpJUm1oZXo3MlIwV2ttSUEKMmp4OE40MzJYZVNBcFo0KzY2YWJrZFN4a1gxd2RRSlZoNWgyYmxidXkrNVNKZzdQWlRCNHVnSDUvUkxXYzM5WgpRdzdTc3pKa09MejFqYVF0ekRIUHFqVFpLQjVpbi93RjBadzRCN2h4R3BUbGx2dnNiSEdtblBsWlRJN1NsZUNGCnNRc2VHakdqZkhWb28xQzczd0l4ZnBkdGFJOUxNVlBlUVlsa3IzeDBHalZXQnNZeE9hRWhKRXFkbUNZVWw4eEoKUkhjbDB6eUw4bFgycytSZm00bEYxaXpRNDVKQit3TUdpYVByY3dJREFRQUJBb0lCQURyWlhLd2s0b2xJQ0NhVApWZmpnTkk1bTBRYkFyRlVuUHVndXJwcCs5cllZYTNZSUpjdFN1c25jWU1aTTFyNkhSSlNSUXVvU0pkbFplNm9pCmJ6SUNGMTZJZVd1Q1REaGR6bGNaTGpKZEIwbEpNTzBDZmlvd01pdGwrRW00TVZrTnErN2hieHoveE1wSENOejcKcG1XNXlGeWpTTk13RmlWZkJRbmtKRWpzU01hc3prSWt2QXFwa21sU2hTRlhLL0FDQzBuZmFYN3M1Q3B2OHpldgpFeFMzSnJRN01FaGJuUTBQSk5JS1A1NE5MdGNDcVl0OVhvUG9BbkRMQW9RNTlGNnpBR0t2eld5Sjk1cHdwb1RwClE1MTNrS1N1bXVhVk5DMW41TDB6UFJTU0FuNlZ5QWM1MUxSSU1rODRQK1ovK1J0OWJ0QmpESTFsM2g4cjRERk8KaXBIajhia0NnWUVBeGhleDNJcVhxWHFURkVSbE1oRThzSWhHSEFVaElHc2Fpa3IxN0RzTjdmYjFhMFE2anhwWApuWGUxdjhEOVhJUGQ2QmVMUEdHSUxFWmZmeGxoaHdRRFd3b1ZjUUQ0YWtMOTJiQXVvSmY5RUlWcWUyTDdkdUVoClRYMy92SjNkWXEwMTV6STltNlYvR3VxaVQwUkRJOXdDMkVza3BZNHowaXZadzZPbVhvaC82V1VDZ1lFQXlMNEMKc0FXS1psS0xMbmVpU1FRRXplSllVYjR3Z2ZjbnBaRWU4TUFTNjJreVAxZ0NRRFdQSHh6KzhjRWpSWXNYUUtXTApnbVdwMVJWdXNhWXAwNGdIRDBJUjhOcVdUR1laWGhBM2J2cjFUSkFjWTdUQ3diUkhkdVRpRVZUYVRmU0RpaEZQCnZsVktlcjRZeER3cUJyM2ZUR0dGTC9iSWduSExjMmtFTEREMG4vY0NnWUVBc3ptMis4SU5MQkt4eGZHSDJYL00KK0MranR6QlE0NExqOVdHVEZWUHM2M080WW4vTnQ3SHV1Wk1ZeHRCMnEyREh3bmlpeWxPNEg4N2dFaC9GcEtIVgo0MlhCTm9mWk9sTTRWOS9Xb0FoRHQ5SHVJSXJTMTZFalAzaVRqSFVNVzM0NXVkOHo3SUlVK1NaM0NkN0tIRVN2CjhrQXlmUE9uSVMzNWpjK2Y5QUh1TVIwQ2dZRUFyb3RJbXZTMldqSDdndlBTejlvR3MxM1RuWC9aZmFnQmVSeXQKNG5lZis4RUVSNyttZFY0Y2k5a1NjL0tUVUt5WUUwWGVBQXVWbUFtQ3JrVGtxV0RsZ29iWVFxeE5jekJ6Ymk1NwpoS3dCRGdsZ0pmSE9SYzhUTkhYZmUySmtUdFFFYTlDUm5kVmJaVTVWQ291bG55Y0pPY2l4bmZyZWJVMjBzU3ptCnkrWGxUaEVDZ1lFQXFvbmtrTGg2WkVEYnNiZzhVN05UMzJ6YVNhWU1WNjRReU5tNGFEK2ZGN2RSd2NEd204QlkKazlBWEhDTE4yQTQxdC8xY2F3UUVBdHNTL2Z2bkJYQ0ZtUGUzWEdEQlBIUE0yNWlLRnozQ1p4RHF4V2Rxa1pyYQpxSXZJUlMxQmZvMFFtN3Z6UTBvTHArMGpMcko3TlhZajBqY1Y0M3BjTXNUa0wvUkN3MVdySyt3PQotLS0tLUVORCBSU0EgUFJJVkFURSBLRVktLS0tLQo="
			tlsCert = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUVtRENDQTRDZ0F3SUJBZ0lVZmNNTFNTbEJZdU5SZlJ2VlFudEFJSVFqSE5Fd0RRWUpLb1pJaHZjTkFRRUwKQlFBd0xqRXNNQ29HQTFVRUF4TWpaV1JuWldOa2JuZ3VZMjl0SUVsdWRHVnliV1ZrYVdGMFpTQkJkWFJvYjNKcApkSGt3SGhjTk1qVXhNREl5TURneE1EUTBXaGNOTWpVeE1USXpNRGd4TVRFMFdqQUFNSUlCSWpBTkJna3Foa2lHCjl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUFtMVdLbnJIczZCaUROODQ1OTh0dVhCdVZ4c3RMSVlrcFdZQmQKSXdVNElZL21QN3hSNXI2ZlF5OWlKeUovM1ZZTmdVMTZianIrK1YwSzFSbFRkOStEODBFc0hsbnM4L2ZIaklSbQpoZXo3MlIwV2ttSUEyang4TjQzMlhlU0FwWjQrNjZhYmtkU3hrWDF3ZFFKVmg1aDJibGJ1eSs1U0pnN1BaVEI0CnVnSDUvUkxXYzM5WlF3N1NzekprT0x6MWphUXR6REhQcWpUWktCNWluL3dGMFp3NEI3aHhHcFRsbHZ2c2JIR20KblBsWlRJN1NsZUNGc1FzZUdqR2pmSFZvbzFDNzN3SXhmcGR0YUk5TE1WUGVRWWxrcjN4MEdqVldCc1l4T2FFaApKRXFkbUNZVWw4eEpSSGNsMHp5TDhsWDJzK1JmbTRsRjFpelE0NUpCK3dNR2lhUHJjd0lEQVFBQm80SUIyakNDCkFkWXdEZ1lEVlIwUEFRSC9CQVFEQWdPb01CTUdBMVVkSlFRTU1Bb0dDQ3NHQVFVRkJ3TUJNQjBHQTFVZERnUVcKQkJTazBOYTgvUThLTWtlRU9lMU9NOUF5NVZsK296QWZCZ05WSFNNRUdEQVdnQlFlTWVaN005M1gyQmZRWU1wTQpSMk9QczRUQXdEQ0J5UVlJS3dZQkJRVUhBUUVFZ2J3d2dia3dSUVlJS3dZQkJRVUhNQUdHT1doMGRIQTZMeTkyCllYVnNkQzUyWVhWc2RDNXpkbU11WTJ4MWMzUmxjaTVzYjJOaGJEbzRNakF3TDNZeEwzQnJhVjlwYm5RdmIyTnoKY0RCd0JnZ3JCZ0VGQlFjd0FvWmthSFIwY0RvdkwzWmhkV3gwTG5aaGRXeDBMbk4yWXk1amJIVnpkR1Z5TG14dgpZMkZzT2pneU1EQXZkakV2Y0d0cFgybHVkQzlwYzNOMVpYSXZOR0ptWVRFMk5HVXRObVE0WWkwNFpEaGtMV05pCk16a3RaV0prTVRGaVpXTXlOMk5rTDJSbGNqQW9CZ05WSFJFQkFmOEVIakFjZ2hwamFHRnNiR1Z1WjJVdVkyUnUKTG1Wa1oyVmpaRzU0TG1OdmJUQjVCZ05WSFI4RWNqQndNRzZnYktCcWhtaG9kSFJ3T2k4dmRtRjFiSFF1ZG1GMQpiSFF1YzNaakxtTnNkWE4wWlhJdWJHOWpZV3c2T0RJd01DOTJNUzl3YTJsZmFXNTBMMmx6YzNWbGNpODBZbVpoCk1UWTBaUzAyWkRoaUxUaGtPR1F0WTJJek9TMWxZbVF4TVdKbFl6STNZMlF2WTNKc0wyUmxjakFOQmdrcWhraUcKOXcwQkFRc0ZBQU9DQVFFQWoyREFwQ2o2cEF1MVkrQ1k4VUFSQ2NMWEtJRFB1QXRlVDZEVHJvVExpOWcrNzB1WQpFWXQxT2ZPaUIrZHBHUWdqejdmN1JZQnZxVWt0RlliWUtWOTIvVEREWS9PQitQTC9HUEJGdVFyR2tYL1JiZEhqCkZqL2pVL0NXNHN0RklEUDJNUHd3Yy9nd2VrcXRJczRvOUQyNUtJN1FSUWNsZ2p6Rml2TnFrWnM5UXc2RStndmYKN2hDeHQvN3pmNUpHMWNHWnViNk9TTWV3ZzhxMk5obFdMM0RQT29zaHhGUDZ4dkt0d1hvWE1CQnJRQUFXcFViRwpLcm1xR0dtMHhmaVlRbmFycUhUaFhUWDhnOHNVTkZGUzlNalNVajY2c0hHVXpPc1IxWTRPM0dxcGUxNEl1bnRUCnhuL2RhOUVnRThvQUt5VTFNVG5GbHhIdTQ3TXN1WWpCVW1KaTZBPT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQotLS0tLUJFR0lOIENFUlRJRklDQVRFLS0tLS0KTUlJRWRUQ0NBMTJnQXdJQkFnSVVIdUMyTzJiQndJd1dKZnZJeHRqdW1QQ0Q1SlF3RFFZSktvWklodmNOQVFFTApCUUF3RnpFVk1CTUdBMVVFQXhNTVpXUm5aV05rYm5ndVkyOXRNQjRYRFRJMU1EWXlOakV6TkRJek5Gb1hEVE13Ck1EWXlOVEV6TkRNd05Gb3dMakVzTUNvR0ExVUVBeE1qWldSblpXTmtibmd1WTI5dElFbHVkR1Z5YldWa2FXRjAKWlNCQmRYUm9iM0pwZEhrd2dnRWlNQTBHQ1NxR1NJYjNEUUVCQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURId0I4bwprbEdpYUtMUXJwYjdHSkZ6Y0V1QVJRc3BPSC9YaEZ6cHJWVzhPMCsxdWJMdGtkc2JxYUU4VkVNOVY4QnVrOCsrCkFyUDBDWTRpdXhjbzNadXlIOVRuVzQ2S2tMYnFMTWw0a0UzSitqU0ttNVpRVzlDQW5MT2o3Q1ZKVnVUOXdXZHAKVTVIQXU0MjdQek0wWm43aUlJUlRDSXZqbTFGNzRYa0U5OEw4REFQUmxQallDclJqNjBPZUIrK2l5VmZvQkh1OQpkQkZCVXp6eHJMWkNOamx2Z1Q1dFVWSzM5Q1Z2Qlo4a0pLQVBqYlk2d2dOWE5tdTdOYUhGeVhYUjBzb2c5ZU1SCnNKVUwzZjBGZ3ZWZjRPZXlheEFXWlVoZ05XQ2JPUitsYmtnUFNRdVg0eGsxZzdEclQ5cHFySGZtcTRrSWVrdjcKeHFtNEdKQy9sR0F5enp3ZkFnTUJBQUdqZ2dHZ01JSUJuREFPQmdOVkhROEJBZjhFQkFNQ0FRWXdEd1lEVlIwVApBUUgvQkFVd0F3RUIvekFkQmdOVkhRNEVGZ1FVSGpIbWV6UGQxOWdYMEdES1RFZGpqN09Fd01Bd0h3WURWUjBqCkJCZ3dGb0FVSTF0MGE4K0hiVXM4Wk8vZ2I2TmhBQkE4Vmkwd2djRUdDQ3NHQVFVRkJ3RUJCSUcwTUlHeE1FRUcKQ0NzR0FRVUZCekFCaGpWb2RIUndPaTh2ZG1GMWJIUXVkbUYxYkhRdWMzWmpMbU5zZFhOMFpYSXViRzlqWVd3NgpPREl3TUM5Mk1TOXdhMmt2YjJOemNEQnNCZ2dyQmdFRkJRY3dBb1pnYUhSMGNEb3ZMM1poZFd4MExuWmhkV3gwCkxuTjJZeTVqYkhWemRHVnlMbXh2WTJGc09qZ3lNREF2ZGpFdmNHdHBMMmx6YzNWbGNpOW1aall5T1RObVlTMHkKTnpFekxXSTVOR0l0WkdWaE9DMHdNekV4WW1aaU1UWmtaV1V2WkdWeU1IVUdBMVVkSHdSdU1Hd3dhcUJvb0dhRwpaR2gwZEhBNkx5OTJZWFZzZEM1MllYVnNkQzV6ZG1NdVkyeDFjM1JsY2k1c2IyTmhiRG80TWpBd0wzWXhMM0JyCmFTOXBjM04xWlhJdlptWTJNamt6Wm1FdE1qY3hNeTFpT1RSaUxXUmxZVGd0TURNeE1XSm1ZakUyWkdWbEwyTnkKYkM5a1pYSXdEUVlKS29aSWh2Y05BUUVMQlFBRGdnRUJBSUpaV2pYalgyTkJIT0Q3bWNkNjVoYVRVYUMyd1RVbwpra3BEcEdIeG5ZTlIyVjlvbjJyNDdsL2ZqUW5OVUV3UGlPYzAwa0RhanZOY3NRMWVYSTdVWk16dU84bEc3NmprCmRpT1RJM3FlMGc2MW5QdzY5OHpROVNJY0pqRGp3ai9KTVBncFg1ZjNRZnF0M0JUajBKOWZHN0lYcFZwQUJzR1EKZHVkeXk2ejZncXdoR3BaY0QyZGJIeFprTllpL2xmNWRRZlE4Z0pLVWZrblB4TUVWWlFIbG1ocDgyd3BHdjl1cAptNFNWaW1ERC8ycGFjK1Y4T2tDU1grMWNjZm9sRHNDNDYyQ2pCaGZ5UVZ4VjJyMXFtUmNNVWNkWTVUcmVwSVNtClE2ZXRtRExyWVVBYU5KN3YzR2tveUg4VzBGbFpiRS9tRDdQa1JqNjBYeG1laHg3M20rN3pYRjg9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0KLS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURPRENDQWlDZ0F3SUJBZ0lVQzRjUkpzb05aOStweVI5ODFNRHJQUU16aXBjd0RRWUpLb1pJaHZjTkFRRUwKQlFBd0Z6RVZNQk1HQTFVRUF4TU1aV1JuWldOa2JuZ3VZMjl0TUI0WERUSTFNRFl5TmpFek16Z3pOMW9YRFRNMQpNRFl5TkRFek16a3dOMW93RnpFVk1CTUdBMVVFQXhNTVpXUm5aV05rYm5ndVkyOXRNSUlCSWpBTkJna3Foa2lHCjl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUF5SnJnZ2N5RWVXZlI2ZllnWE4zR294MFloWHVFZC9OTEk2bjMKRWRyTmJSN2NUcjNjOVVQYVZ5UzYvQzJySHIvVUoweDlwQUNqK01SU1NleHV5QVRQSUFnTkJqeGRha0k3ZEhlWQo1bFlDTnJKYUMrdUJWeHJlNUYyYlN2QUJOSDViVkdXY2I0ZG9RSVk0RDVjc2ovVW8zWkw3TE9JY1dRTmFwT0FoCmVoK0t2M1E5WnJqSkFEM0RmNzl4bnYxcUl2RW9uNi9OOGpiM09FUjNod3dOYmYzc3dKMmFiYXZ6akVic0RYYmwKRGtPNFlNVDVUZkMvNzQ1UUM3WWh0UUZ6WjYwUjVVNnVMZkxvUnRuQUZxYjZrS2JwVnJUOEZoUENXVkJXUTFCeQpJSjc2K3NjWVJ5dzRQK0JKNHdLWDQ5QmVNWjd6MUYvY2dVb1pyRk15aEM5amZ5TUZHd0lEQVFBQm8zd3dlakFPCkJnTlZIUThCQWY4RUJBTUNBUVl3RHdZRFZSMFRBUUgvQkFVd0F3RUIvekFkQmdOVkhRNEVGZ1FVSTF0MGE4K0gKYlVzOFpPL2diNk5oQUJBOFZpMHdId1lEVlIwakJCZ3dGb0FVSTF0MGE4K0hiVXM4Wk8vZ2I2TmhBQkE4VmkwdwpGd1lEVlIwUkJCQXdEb0lNWldSblpXTmtibmd1WTI5dE1BMEdDU3FHU0liM0RRRUJDd1VBQTRJQkFRQjkrbGpvCnRpeURnN0p4WVp4QzV6ZEVhaHoxNksvK0NYME53Z2ZTNGxnd1RFVDFMR2tVR1g1Um9SeVBXallZb2dqTlc5OEsKZUl3c2NMMlpNeUNnRUJXUCtpcFkrRVA4cFcrOXFSQWc5YVFwVkZ3U1FvS09raTJ3VUtkQklvaG8wVXBvQ0hVQwpHZ2ZBRkt6NEFYcCtqN2x0VTFQdWlCanpVQWZVUTlvOTlMenlNamhuY2tXekFQWFBqSG9CM0ZVb0pJR3owUTRPCmRRelVyN0plVHpmZkJjcFJUTExZUlpCdlAxVzlLUUd2bWczTUcvb01OQ3l1a0ZOa1Jub1l1Zll2QVJnbU9Bc0MKTWhBYStRM0MwZU1WNWt0S28yRkQzM3ZwQVVlSzMrT1RIdGNUWjI5bGMwYVhUUW8yY21JYlNDQTFQMWRFNzM2VAozcHRaL0FkZFNXZ3l6SkFoCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K"
		)

		// StaticService

		serviceLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}
		service := &infrastructurev1alpha1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ServiceName,
				Namespace: Namespace,
			},
			Spec: infrastructurev1alpha1.ServiceSpec{
				Name:       ServiceName,
				Domain:     ServiceName,
				OriginType: "static",
				StaticOrigins: []infrastructurev1alpha1.StaticOriginSpec{
					{
						Upstream:   "randomupstream.com",
						Port:       443,
						HostHeader: "randomupstream.com",
						Scheme:     "Https",
					},
				},
			},
		}

		BeforeEach(func() {
			role := os.Getenv("ROLE")
			if role != ControllerRole {
				Skip("Skipping test because ROLE is not 'controller'")
			}

			newService := &infrastructurev1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ServiceName,
					Namespace: Namespace,
				},
				Spec: service.Spec,
			}

			Expect(k8sClient.Create(ctx, newService)).To(Succeed())
			createdService := &infrastructurev1alpha1.Service{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceLookupKey, createdService)).To(Succeed())
				g.Expect(createdService.Status.Status).To(Equal("Healthy"))
			}, timeout, interval).Should(Succeed())
		})

		AfterEach(func() {
			role := os.Getenv("ROLE")
			if role != ControllerRole {
				Skip("Skipping test because ROLE is not 'controller'")
			}

			createdService := &infrastructurev1alpha1.Service{}
			Expect(k8sClient.Get(ctx, serviceLookupKey, createdService)).To(Succeed())
			Expect(k8sClient.Delete(ctx, createdService)).To(Succeed())

			By("Waiting for Service to be deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, serviceLookupKey, createdService)
				if err != nil {
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(createdService.DeletionTimestamp.IsZero()).To(BeFalse())
				}
			}, timeout, interval).Should(Succeed())

			// Owned resources are not automatically deleted in test env. Clean up manually
			cert := &certmanagerv1.Certificate{}
			certLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}
			By("Deleting Certificate")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, certLookupKey, cert)
				if err != nil {
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(k8sClient.Delete(ctx, cert)).To(Succeed())
				}
			}, timeout, interval).Should(Succeed())

			// Owned resources are not automatically deleted in test env. Clean up manually
			appset := &argoprojv1alpha1.ApplicationSet{}
			appsetLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}
			By("Deleting ApplicationSet")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, appsetLookupKey, appset)
				if err != nil {
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(k8sClient.Delete(ctx, appset)).To(Succeed())
				}
			}, timeout, interval).Should(Succeed())
		})

		Context("When Creating a Service", func() {
			It("Should set status to Healthy, Create a certificate and ApplicationSet", func() {
				By("Checking that Service is healthy")

				createdService := &infrastructurev1alpha1.Service{}
				Expect(k8sClient.Get(ctx, serviceLookupKey, createdService)).To(Succeed())
				Expect(createdService.Status.Status).To(Equal("Healthy"))

				By("Checking that Service Spec was set correctly")
				Expect(createdService.Spec.Name).To(Equal(ServiceName))
				Expect(createdService.Spec.Domain).To(Equal(ServiceName))
				Expect(createdService.Spec.OriginType).To(Equal("static"))
				Expect(createdService.Spec.StaticOrigins).To(HaveLen(1))
				Expect(createdService.Spec.StaticOrigins[0].Upstream).To(Equal("randomupstream.com"))
				Expect(createdService.Spec.StaticOrigins[0].Port).To(Equal(443))
				Expect(createdService.Spec.StaticOrigins[0].HostHeader).To(Equal("randomupstream.com"))
				Expect(createdService.Spec.StaticOrigins[0].Scheme).To(Equal("Https"))

				By("Checking that Certificate was created")
				cert := &certmanagerv1.Certificate{}
				certLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}

				Expect(k8sClient.Get(ctx, certLookupKey, cert)).To(Succeed())
				Expect(cert.Spec.DNSNames).To(ContainElement(ServiceName))

				By("Checking that Certificate has correct Hash annotation")
				certBuilder, err := builder.CertBuilderFactory("Service", service.Name, service.Namespace, service.Spec.Domain, cmmeta.ObjectReference{
					Name:  ClusterIssuerName,
					Kind:  "ClusterIssuer",
					Group: certmanagerv1.SchemeGroupVersion.Group,
				})
				Expect(err).ToNot(HaveOccurred())

				_, hash, err := certBuilder.Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(cert.ObjectMeta.Annotations[builder.ValuesHashAnnotation]).To(Equal(hash))

				By("Checking that ApplicationSet was created")
				appset := &argoprojv1alpha1.ApplicationSet{}
				appsetLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}

				Expect(k8sClient.Get(ctx, appsetLookupKey, appset)).To(Succeed())

				By("Checking that ApplicationSet has correct Hash annotation")
				appsetBuilder, err := builder.AppsetBuilderFactory("Service", ServiceName, Namespace, builder.ThrowerOptions{
					ThrowerChartRepository: ThrowerChartRepository,
					ThrowerChartName:       ThrowerChartName,
					ThrowerChartVersion:    ThrowerChartVersion,
					TargetNamespace:        InfrastructureTargetNamespace,
					ApplicationSetProject:  InfrastructureApplicationSetProject,
				})
				Expect(err).ToNot(HaveOccurred())

				serviceRawResource := &infrastructurev1alpha1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "infrastructure.edgecdnx.com/v1alpha1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: createdService.Name,
					},
					Spec: createdService.Spec,
				}

				serviceHelmValues := struct {
					Resources []any `json:"resources"`
				}{
					Resources: []any{serviceRawResource},
				}
				appsetBuilder.SetHelmValues(serviceHelmValues)
				_, hash, err = appsetBuilder.Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(appset.ObjectMeta.Annotations[builder.ValuesHashAnnotation]).To(Equal(hash))
			})
		})

		Context("When Creating a Service and Setting a TLS key", func() {
			It("Should create a certificate with the specified secret name", func() {

				cert := &certmanagerv1.Certificate{}
				certLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}

				Expect(k8sClient.Get(ctx, certLookupKey, cert)).To(Succeed())

				decodedCert, err := base64.StdEncoding.DecodeString(tlsCert)
				Expect(err).ToNot(HaveOccurred())
				decodedKey, err := base64.StdEncoding.DecodeString(tlsKey)
				Expect(err).ToNot(HaveOccurred())

				tlsSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cert.Spec.SecretName,
						Namespace: Namespace,
					},
					Data: map[string][]byte{
						"tls.crt": decodedCert,
						"tls.key": decodedKey,
					},
					Type: corev1.SecretTypeTLS,
				}
				Expect(k8sClient.Create(ctx, tlsSecret)).To(Succeed())

				certCondition := certmanagerv1.CertificateCondition{
					Type:   certmanagerv1.CertificateConditionReady,
					Status: cmmeta.ConditionTrue,
				}

				cert.Status.Conditions = []certmanagerv1.CertificateCondition{certCondition}
				Expect(k8sClient.Status().Update(ctx, cert)).To(Succeed())

				By("Verifying that Service got Secret Populated")
				Eventually(func(g Gomega) {
					appset := &argoprojv1alpha1.ApplicationSet{}
					appsetLookupKey := types.NamespacedName{Name: ServiceName, Namespace: Namespace}
					g.Expect(k8sClient.Get(ctx, appsetLookupKey, appset)).To(Succeed())

					var raw struct {
						Resources []infrastructurev1alpha1.Service `json:"resources"`
					}
					yaml.Unmarshal(appset.Spec.Template.Spec.Sources[0].Helm.ValuesObject.Raw, &raw)
					g.Expect(raw.Resources[0].Spec.Certificate.Crt).To(Equal(tlsCert))
					g.Expect(raw.Resources[0].Spec.Certificate.Key).To(Equal(tlsKey))
				}, timeout, interval).Should(Succeed())
			})
		})
	})
})
