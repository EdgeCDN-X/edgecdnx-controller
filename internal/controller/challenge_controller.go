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
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LocationReconciler reconciles a Location object
type ChallengeReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ThrowerOptions builder.ThrowerOptions
}

// GetOwnerInChain traverses the ownerReferences chain starting from obj and attempts to find and fetch
// an object of the given targetKind into targetObj. The fetch is performed using the provided client.
func GetOwnerInChain[
	T client.Object,
](ctx context.Context, c client.Client, obj client.Object, reqNamespace string, targetKind string, targetObj T) error {
	currObj := obj
	for {
		owners := currObj.GetOwnerReferences()
		found := false
		for _, owner := range owners {
			if owner.Kind == targetKind && owner.Controller != nil && *owner.Controller {
				if err := c.Get(ctx, types.NamespacedName{
					Name:      owner.Name,
					Namespace: reqNamespace,
				}, targetObj); err != nil {
					logf.FromContext(ctx).Error(err, "Failed to get object", "kind", targetKind)
					return err
				}
				return nil
			}
			// Traverse to next owner if not found yet
			if owner.Controller != nil && *owner.Controller {
				// Dynamically create the next object based on kind
				var nextObj client.Object
				switch owner.Kind {
				case "Order":
					nextObj = &acmev1.Order{}
				case "CertificateRequest":
					nextObj = &certmanagerv1.CertificateRequest{}
				case "Certificate":
					nextObj = &certmanagerv1.Certificate{}
				case "Service":
					nextObj = &infrastructurev1alpha1.Service{}
				default:
					continue
				}
				if err := c.Get(ctx, types.NamespacedName{
					Name:      owner.Name,
					Namespace: reqNamespace,
				}, nextObj); err != nil {
					logf.FromContext(ctx).Error(err, "Failed to get object", "kind", owner.Kind)
					return err
				}
				currObj = nextObj
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return errors.New("No object of kind " + targetKind + " found in owner chain")
}

// +kubebuilder:rbac:groups=acme.cert-manager.io/v1,resources=challenges,verbs=get;list;watch
// +kubebuilder:rbac:groups=acme.cert-manager.io/v1,resources=orders,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io/v1,resources=certificaterequests,verbs=get;list;watch
// +kubebuilder:rbac:groups=acme.cert-manager.io/v1,resources=challenges/status,verbs=get
// +kubebuilder:rbac:groups=argoproj.io,resources=applicationsets,verbs=get;list;watch;create;update;patch;delete

func (r *ChallengeReconciler) getChallengeResources(challenge *acmev1.Challenge, serviceOwner *infrastructurev1alpha1.Service) (corev1.Pod, corev1.Service, networkingv1.Ingress, error) {
	challengeSpec, err := json.Marshal(challenge.Spec)
	if err != nil {
		return corev1.Pod{}, corev1.Service{}, networkingv1.Ingress{}, err
	}
	md5Hash := md5.Sum(challengeSpec)

	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      challenge.Name,
			Namespace: r.ThrowerOptions.TargetNamespace,
			Annotations: map[string]string{
				"cluster-autoscaler.kubernetes.io/safe-to-evict": "true",
				"sidecar.istio.io/inject":                        "false",
			},
			Labels: map[string]string{
				"app":  challenge.Name,
				"hash": fmt.Sprintf("%x", md5Hash),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "acmesolver",
					Image: "quay.io/jetstack/cert-manager-acmesolver:v1.17.2",
					Args: []string{
						"--listen-port=8089",
						fmt.Sprintf("--domain=%s", challenge.Spec.DNSName),
						fmt.Sprintf("--token=%s", challenge.Spec.Token),
						fmt.Sprintf("--key=%s", challenge.Spec.Key),
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8089,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
			NodeSelector: map[string]string{"kubernetes.io/os": "linux"},
		},
	}

	serviceName := strings.ReplaceAll(challenge.Name, ".", "-")

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: r.ThrowerOptions.TargetNamespace,
			Labels: map[string]string{
				"app":  challenge.Name,
				"hash": fmt.Sprintf("%x", md5Hash),
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":  challenge.Name,
				"hash": fmt.Sprintf("%x", md5Hash),
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8089,
					TargetPort: intstr.FromInt(8089),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific

	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      challenge.Name,
			Namespace: r.ThrowerOptions.TargetNamespace,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/whitelist-source-range": "0.0.0.0/0,::/0",
			},
			Labels: map[string]string{
				"app":  challenge.Name,
				"hash": fmt.Sprintf("%x", md5Hash),
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &serviceOwner.Spec.Cache,
			Rules: []networkingv1.IngressRule{
				{
					Host: challenge.Spec.DNSName,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     fmt.Sprintf("/.well-known/acme-challenge/%s", challenge.Spec.Token),
									PathType: &pathTypeImplementationSpecific,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: serviceName,
											Port: networkingv1.ServiceBackendPort{
												Number: 8089,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return *pod, *service, *ingress, nil
}

func (r *ChallengeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	challenge := &acmev1.Challenge{}
	if err := r.Get(ctx, req.NamespacedName, challenge); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "Failed to get Challenge")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	serviceOwner := &infrastructurev1alpha1.Service{}
	err := GetOwnerInChain(ctx, r.Client, challenge, req.Namespace, "Service", serviceOwner)
	if err != nil {
		log.Error(err, "Failed to get Service owner for Challenge")
		return ctrl.Result{}, err
	}

	pod, service, ingress, err := r.getChallengeResources(challenge, serviceOwner)
	if err != nil {
		log.Error(err, "Failed to get Challenge resources")
		return ctrl.Result{}, err
	}

	if challenge.Status.Presented && challenge.Status.Processing {
		challengeSolverHelmValues := struct {
			Resources []any `json:"resources"`
		}{
			Resources: []any{pod, service, ingress},
		}

		challengeSolverBuilder, err := builder.AppsetBuilderFactory("Challenge", challenge.Name, req.Namespace, r.ThrowerOptions)
		if err != nil {
			log.Error(err, "Failed to create ApplicationSet builder for Challenge")
			return ctrl.Result{}, err
		}
		challengeSolverBuilder.WithHelmValues(challengeSolverHelmValues)

		desiredAppset, hash, err := challengeSolverBuilder.Build()
		if err != nil {
			log.Error(err, "Failed to get ApplicationSet spec for Challenge")
			return ctrl.Result{}, err
		}

		// check if we have a corresponding Applicationset
		currAppset := &argoprojv1alpha1.ApplicationSet{}

		if err := r.Get(ctx, client.ObjectKey{
			Name:      desiredAppset.Name,
			Namespace: desiredAppset.Namespace,
		}, currAppset); err != nil {
			if client.IgnoreNotFound(err) != nil {
				log.Error(err, "Failed to get ApplicationSet for Challenge")
				return ctrl.Result{}, err
			}
			log.Info("No ApplicationSet found for Challenge, creating one")

			// Create ApplicationSet logic here
			err := controllerutil.SetControllerReference(challenge, &desiredAppset, r.Scheme)
			if err != nil {
				log.Error(err, "Failed to set controller reference on ApplicationSet")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, r.Create(ctx, &desiredAppset)

		} else {
			log.Info("ApplicationSet already exists for Challenge", "name", currAppset.Name)
			// Update ApplicationSet logic here if needed
			if !currAppset.DeletionTimestamp.IsZero() {
				log.Info("ApplicationSet for challenge is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currAppsetHash, ok := currAppset.Annotations[builder.ValuesHashAnnotation]
			if !ok || currAppsetHash != hash {
				log.Info("Updating ApplicationSet for challenge", "name", challenge.Name)
				currAppset.Spec = desiredAppset.Spec
				currAppset.Annotations = desiredAppset.Annotations
				return ctrl.Result{}, r.Update(ctx, currAppset)
			}
		}
	} else {
		log.Info(fmt.Sprintf("Challenge status %v. Not reconciling", challenge.Status))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ChallengeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&acmev1.Challenge{}).
		Complete(r)
}
