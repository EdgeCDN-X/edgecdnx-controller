package tenancy

import (
	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s Scope) CacheObjects() map[client.Object]cache.ByObject {
	if s.Namespaced {
		return nil
	}
	objects := make(map[client.Object]cache.ByObject)
	for _, object := range []client.Object{
		&api.Project{}, &api.APIKey{}, &api.DNSEndpoint{}, &api.HealthCheckProfile{},
		&api.Location{}, &api.PrefixList{}, &api.Service{}, &api.Zone{},
	} {
		objects[object] = cache.ByObject{Namespaces: map[string]cache.Config{s.TargetNamespace: {}}}
	}
	return objects
}
