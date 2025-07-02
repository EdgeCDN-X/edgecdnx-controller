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
	"fmt"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	consulapi "github.com/hashicorp/consul/api"
)

// LocationRoutingReconciler reconciles a Location object
type LocationRoutingReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ConsulEndpoint string
	consulClient   *consulapi.Client
}

func (r *LocationRoutingReconciler) getConsulNodeInstance(location *infrastructurev1alpha1.Location, idx int) (consulapi.CatalogRegistration, map[string]string, string, error) {

	nodeName := fmt.Sprintf("%s.%s.edgecdnx.com", location.Spec.Nodes[idx].Name, location.Name)

	marshable := struct {
		Spec consulapi.CatalogRegistration `json:"spec"`
		Meta map[string]string             `json:"meta"`
	}{
		Spec: consulapi.CatalogRegistration{
			Node:    nodeName,
			Address: location.Spec.Nodes[idx].Ipv4,
			Service: &consulapi.AgentService{
				ID:      fmt.Sprintf("cache-%s", nodeName),
				Service: fmt.Sprintf("cache-%s", nodeName),
				Address: location.Spec.Nodes[idx].Ipv4,
				Port:    80,
			},
			Checks: []*consulapi.HealthCheck{
				{
					CheckID:   fmt.Sprintf("service:cache-%s", nodeName),
					ServiceID: fmt.Sprintf("cache-%s", nodeName),
					Definition: consulapi.HealthCheckDefinition{
						HTTP:             fmt.Sprintf("http://%s/healthz", location.Spec.Nodes[idx].Ipv4),
						IntervalDuration: 30 * time.Second,
					},
				},
			},
		},
		Meta: map[string]string{
			"external-node":  "true",
			"external-probe": "true",
			"location":       location.Name,
		},
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return consulapi.CatalogRegistration{}, nil, "", fmt.Errorf("failed to marshal consul node instance: %w", err)
	}
	return marshable.Spec, marshable.Meta, fmt.Sprintf("%x", md5.Sum(hashable)), nil

}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Location object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *LocationRoutingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	location := &infrastructurev1alpha1.Location{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, location); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	finalizerName := "edgecdnx.com/location-finalizer"

	if location.ObjectMeta.DeletionTimestamp.IsZero() {
		// Not being deleted, ensure finalizer is present
		if !controllerutil.ContainsFinalizer(location, finalizerName) {
			log.Info("Adding finalizer to Location")
			controllerutil.AddFinalizer(location, finalizerName)
			return ctrl.Result{}, r.Update(ctx, location)
		}
	} else {
		// Being deleted, handle finalizer logic
		if controllerutil.ContainsFinalizer(location, finalizerName) {
			for node := range location.Spec.Nodes {
				nodeName := fmt.Sprintf("%s.%s.edgecdnx.com", location.Spec.Nodes[node].Name, location.Name)
				log.Info(fmt.Sprintf("Removing node from Consul - %s", nodeName), "Node", nodeName)
				_, err := r.consulClient.Catalog().Deregister(&consulapi.CatalogDeregistration{
					Node: nodeName,
				}, &consulapi.WriteOptions{})
				if err != nil {
					log.Error(err, "Failed to deregister node in Consul", "Node", nodeName)
					return ctrl.Result{}, err
				}
				log.Info("Node removed from Consul", "Node", nodeName)
			}

			// ArgoCD is likely modifying the object, so we need to re-fetch it
			if err := r.Get(ctx, req.NamespacedName, location); err != nil {
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}

			removed := controllerutil.RemoveFinalizer(location, finalizerName)
			if removed {
				log.Info("Removing finalizer from Location")
				return ctrl.Result{}, r.Update(ctx, location)
			}
		}
		return ctrl.Result{}, nil
	}

	if location.Status != (infrastructurev1alpha1.LocationStatus{}) {
		for node := range location.Spec.Nodes {
			nodeName := fmt.Sprintf("%s.%s.edgecdnx.com", location.Spec.Nodes[node].Name, location.Name)
			consulNode, _, err := r.consulClient.Catalog().Node(nodeName, &consulapi.QueryOptions{})

			if err != nil {
				log.Error(err, "Failed to get node from Consul")
				return ctrl.Result{}, err
			}

			if consulNode == nil {
				log.Info(fmt.Sprintf("Node %s not found in Consul. Registering", nodeName))

				registration, meta, hash, err := r.getConsulNodeInstance(location, node)
				if err != nil {
					log.Error(err, "Failed to get Consul node instance")
					return ctrl.Result{}, err
				}

				uid, err := uuid.NewUUID()
				if err != nil {
					log.Error(err, "Failed to generate UUID for Consul node registration")
					return ctrl.Result{}, err
				}

				registration.ID = uid.String()
				registration.NodeMeta = meta
				registration.NodeMeta[ValuesHashAnnotation] = hash

				_, err = r.consulClient.Catalog().Register(&registration, &consulapi.WriteOptions{})

				if err != nil {
					log.Error(err, "Failed to register node in Consul")
					return ctrl.Result{}, err
				}
			} else {

				registration, meta, hash, err := r.getConsulNodeInstance(location, node)

				if err != nil {
					log.Error(err, "Failed to get Consul node instance")
					return ctrl.Result{}, err
				}

				curHash, ok := consulNode.Node.Meta[ValuesHashAnnotation]

				if !ok || curHash != hash {
					_, err := r.consulClient.Catalog().Deregister(&consulapi.CatalogDeregistration{
						Node: nodeName,
					}, &consulapi.WriteOptions{})

					if err != nil {
						log.Error(err, "Failed to deregister node in Consul")
						return ctrl.Result{}, err
					}

					log.Info(fmt.Sprintf("Node %s found in Consul but hash mismatch. Deregistering and re-registering", nodeName))

					uid, err := uuid.NewUUID()
					if err != nil {
						log.Error(err, "Failed to generate UUID for Consul node registration")
						return ctrl.Result{}, err
					}

					registration.ID = uid.String()
					registration.NodeMeta = meta
					registration.NodeMeta[ValuesHashAnnotation] = hash

					_, err = r.consulClient.Catalog().Register(&registration, &consulapi.WriteOptions{})

					if err != nil {
						log.Error(err, "Failed to register node in Consul")
						return ctrl.Result{}, err
					}
				}
			}
		}

		consulNodes, _, err := r.consulClient.Catalog().Nodes(&consulapi.QueryOptions{
			Filter: fmt.Sprintf("Meta.location == \"%s\"", location.Name),
		})

		if err != nil {
			log.Error(err, "Failed to get nodes from Consul")
			return ctrl.Result{}, err
		}

		for _, consulNode := range consulNodes {
			nodeFound := false
			for _, node := range location.Spec.Nodes {
				nodeName := fmt.Sprintf("%s.%s.edgecdnx.com", node.Name, location.Name)
				if consulNode.Node == nodeName {
					nodeFound = true
					break
				}
			}

			if !nodeFound {
				log.Info("Node found in Consul but not in Location spec. Deregistering", "Node", consulNode.Node)
				_, err := r.consulClient.Catalog().Deregister(&consulapi.CatalogDeregistration{
					Node: consulNode.Node,
				}, &consulapi.WriteOptions{})
				if err != nil {
					log.Error(err, "Failed to deregister node in Consul", "Node", consulNode.Node)
					return ctrl.Result{}, err
				}
			}
		}

		location.Status = infrastructurev1alpha1.LocationStatus{
			Status: HealthStatusHealthy,
		}

		return ctrl.Result{}, r.Status().Update(ctx, location)
	} else {
		location.Status = infrastructurev1alpha1.LocationStatus{
			Status: HealthStatusProgressing,
		}
		return ctrl.Result{}, r.Status().Update(ctx, location)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *LocationRoutingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	config := consulapi.DefaultConfig()
	config.Address = r.ConsulEndpoint
	consul, err := consulapi.NewClient(config)
	if err != nil {
		logf.Log.Error(err, "Failed to create Consul client")
		return err
	}

	r.consulClient = consul

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Location{}).
		Complete(r)
}
