package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type DownstreamTarget string

const (
	DownstreamTargetRouter DownstreamTarget = "router"
	DownstreamTargetCache  DownstreamTarget = "cache"

	DownstreamManagedLabel = "downstream.edgecdnx.com/source-id"
	DownstreamWorkers      = 16
	DownstreamPageSize     = 500
)

type downstreamResourceKind struct {
	name      string
	newObject func() client.Object
	newList   func() client.ObjectList
}

type downstreamQueueKey struct {
	Kind string
	Key  types.NamespacedName
}

type DownstreamReconciler struct {
	TargetNamespace string
	UpstreamCache   ctrlcache.Cache
	UpstreamReader  client.Reader
	Local           client.Client
	Target          DownstreamTarget
	SourceID        string
	Workers         int
	queue           workqueue.TypedRateLimitingInterface[downstreamQueueKey]
	kinds           map[string]downstreamResourceKind
	namespaces      sync.Map
}

var stripManagedFields = ctrlcache.TransformStripManagedFields()

func NewDownstreamReconciler(upstreamCache ctrlcache.Cache, local client.Client, target DownstreamTarget, sourceID string, workers int) (*DownstreamReconciler, error) {
	kinds, err := downstreamResourceKinds(target)
	if err != nil {
		return nil, err
	}
	if upstreamCache == nil || local == nil {
		return nil, errors.New("downstream upstream cache and local client must be configured")
	}
	if sourceID == "" {
		return nil, errors.New("downstream source ID must be configured")
	}
	if workers < 1 {
		workers = DownstreamWorkers
	}

	reconciler := &DownstreamReconciler{
		UpstreamCache:  upstreamCache,
		UpstreamReader: upstreamCache,
		Local:          local,
		Target:         target,
		SourceID:       sourceID,
		Workers:        workers,
		queue:          workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[downstreamQueueKey]()),
		kinds:          make(map[string]downstreamResourceKind, len(kinds)),
	}
	for _, kind := range kinds {
		reconciler.kinds[kind.name] = kind
	}
	return reconciler, nil
}

func (r *DownstreamReconciler) NeedLeaderElection() bool {
	return true
}

func (r *DownstreamReconciler) RegisterHandlers(ctx context.Context) error {
	for _, kind := range r.kinds {
		informer, err := r.UpstreamCache.GetInformer(ctx, kind.newObject())
		if err != nil {
			return fmt.Errorf("get upstream %s informer: %w", kind.name, err)
		}
		kindName := kind.name
		_, err = informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			AddFunc: func(object interface{}) {
				r.enqueueObject(kindName, object)
			},
			UpdateFunc: func(oldObject, newObject interface{}) {
				oldResource, oldOK := oldObject.(client.Object)
				newResource, newOK := newObject.(client.Object)
				if oldOK && newOK && downstreamSourceEqual(oldResource, newResource) {
					return
				}
				r.enqueueObject(kindName, newObject)
			},
			DeleteFunc: func(object interface{}) {
				r.enqueueObject(kindName, object)
			},
		})
		if err != nil {
			return fmt.Errorf("register upstream %s event handler: %w", kind.name, err)
		}
	}
	return nil
}

func (r *DownstreamReconciler) enqueueObject(kind string, object interface{}) {
	if tombstone, ok := object.(toolscache.DeletedFinalStateUnknown); ok {
		object = tombstone.Obj
	}
	if tombstone, ok := object.(*toolscache.DeletedFinalStateUnknown); ok {
		object = tombstone.Obj
	}
	resource, ok := object.(client.Object)
	if !ok {
		return
	}
	if r.TargetNamespace != "" && resource.GetNamespace() != r.TargetNamespace {
		return
	}
	r.queue.Add(downstreamQueueKey{Kind: kind, Key: client.ObjectKeyFromObject(resource)})
}

func downstreamSourceEqual(oldObject, newObject client.Object) bool {
	oldCopy := oldObject.DeepCopyObject().(client.Object)
	newCopy := newObject.DeepCopyObject().(client.Object)
	clearRemoteMetadata(oldCopy)
	clearRemoteMetadata(newCopy)
	setEmptyStatus(oldCopy)
	setEmptyStatus(newCopy)
	return reflect.DeepEqual(oldCopy, newCopy)
}

func DownstreamCacheTransform(object interface{}) (interface{}, error) {
	transformed, err := stripManagedFields(object)
	if err != nil {
		return nil, err
	}
	if resource, ok := transformed.(client.Object); ok {
		setEmptyStatus(resource)
	}
	return transformed, nil
}

func (r *DownstreamReconciler) Start(ctx context.Context) error {
	cacheErrors := make(chan error, 1)
	go func() {
		cacheErrors <- r.UpstreamCache.Start(ctx)
	}()

	if !r.UpstreamCache.WaitForCacheSync(ctx) {
		select {
		case err := <-cacheErrors:
			if err != nil {
				return fmt.Errorf("upstream informer cache stopped before syncing: %w", err)
			}
			return nil
		default:
		}
		return errors.New("upstream informer caches failed to sync")
	}
	if err := r.enqueueExistingMirrors(ctx); err != nil {
		return fmt.Errorf("enqueue existing downstream mirrors: %w", err)
	}

	var workers sync.WaitGroup
	for i := 0; i < r.Workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.runWorker(ctx)
		}()
	}
	select {
	case <-ctx.Done():
	case err := <-cacheErrors:
		if err != nil {
			return fmt.Errorf("upstream informer cache stopped: %w", err)
		}
		return errors.New("upstream informer cache stopped unexpectedly")
	}
	r.queue.ShutDown()
	workers.Wait()
	return nil
}

func (r *DownstreamReconciler) runWorker(ctx context.Context) {
	for {
		key, shutdown := r.queue.Get()
		if shutdown {
			return
		}
		err := r.reconcileKey(ctx, key)
		r.queue.Done(key)
		if err != nil {
			log.FromContext(ctx).Error(err, "downstream reconcile failed", "kind", key.Kind, "namespace", key.Key.Namespace, "name", key.Key.Name)
			r.queue.AddRateLimited(key)
			continue
		}
		r.queue.Forget(key)
	}
}

func downstreamResourceKinds(target DownstreamTarget) ([]downstreamResourceKind, error) {
	switch target {
	case DownstreamTargetRouter:
		return []downstreamResourceKind{
			{name: "Location", newObject: func() client.Object { return &infrastructurev1alpha1.Location{} }, newList: func() client.ObjectList { return &infrastructurev1alpha1.LocationList{} }},
			{name: "HealthCheckProfile", newObject: func() client.Object { return &infrastructurev1alpha1.HealthCheckProfile{} }, newList: func() client.ObjectList { return &infrastructurev1alpha1.HealthCheckProfileList{} }},
			{name: "Zone", newObject: func() client.Object { return &infrastructurev1alpha1.Zone{} }, newList: func() client.ObjectList { return &infrastructurev1alpha1.ZoneList{} }},
			{name: "PrefixList", newObject: func() client.Object { return &infrastructurev1alpha1.PrefixList{} }, newList: func() client.ObjectList { return &infrastructurev1alpha1.PrefixListList{} }},
			{name: "DNSEndpoint", newObject: func() client.Object { return &infrastructurev1alpha1.DNSEndpoint{} }, newList: func() client.ObjectList { return &infrastructurev1alpha1.DNSEndpointList{} }},
		}, nil
	case DownstreamTargetCache:
		return []downstreamResourceKind{
			{name: "Service", newObject: func() client.Object { return &infrastructurev1alpha1.Service{} }, newList: func() client.ObjectList { return &infrastructurev1alpha1.ServiceList{} }},
		}, nil
	default:
		return nil, fmt.Errorf("invalid downstream target %q: must be %q or %q", target, DownstreamTargetRouter, DownstreamTargetCache)
	}
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;create
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints;healthcheckprofiles;locations;prefixlists;services;zones,verbs=get;list;create;update;delete

func (r *DownstreamReconciler) reconcileKey(ctx context.Context, key downstreamQueueKey) error {
	if r.TargetNamespace != "" && key.Key.Namespace != r.TargetNamespace {
		return nil
	}
	kind, exists := r.kinds[key.Kind]
	if !exists {
		return fmt.Errorf("unknown downstream resource kind %q", key.Kind)
	}
	source := kind.newObject()
	if err := r.UpstreamReader.Get(ctx, key.Key, source); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get upstream %s: %w", key.Kind, err)
		}
		return r.deleteMirror(ctx, kind, key.Key)
	}
	if err := r.ensureNamespace(ctx, source.GetNamespace()); err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}
	if err := r.upsert(ctx, kind, source); err != nil {
		return fmt.Errorf("upsert mirror: %w", err)
	}
	return nil
}

func (r *DownstreamReconciler) enqueueExistingMirrors(ctx context.Context) error {
	for _, kind := range r.kinds {
		continueToken := ""
		for {
			list := kind.newList()
			if err := r.Local.List(ctx, list,
				client.InNamespace(r.TargetNamespace),
				client.MatchingLabels{DownstreamManagedLabel: r.SourceID},
				client.Limit(DownstreamPageSize),
				client.Continue(continueToken),
			); err != nil {
				return fmt.Errorf("list local %s mirrors: %w", kind.name, err)
			}
			items, err := meta.ExtractList(list)
			if err != nil {
				return fmt.Errorf("extract local %s mirrors: %w", kind.name, err)
			}
			for _, item := range items {
				resource, ok := item.(client.Object)
				if !ok {
					return fmt.Errorf("local %s list contains a non-client object", kind.name)
				}
				r.enqueueObject(kind.name, resource)
			}
			listMeta, err := meta.ListAccessor(list)
			if err != nil {
				return fmt.Errorf("read local %s continue token: %w", kind.name, err)
			}
			continueToken = listMeta.GetContinue()
			if continueToken == "" {
				break
			}
		}
	}
	return nil
}

func (r *DownstreamReconciler) ensureNamespace(ctx context.Context, namespace string) error {
	if namespace == "" {
		return errors.New("source resource has an empty namespace")
	}
	if _, exists := r.namespaces.Load(namespace); exists {
		return nil
	}

	localNamespace := &corev1.Namespace{}
	if err := r.Local.Get(ctx, types.NamespacedName{Name: namespace}, localNamespace); err == nil {
		r.namespaces.Store(namespace, struct{}{})
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	if err := r.Local.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	r.namespaces.Store(namespace, struct{}{})
	return nil
}

func (r *DownstreamReconciler) upsert(ctx context.Context, kind downstreamResourceKind, source client.Object) error {
	desired := source.DeepCopyObject().(client.Object)
	clearRemoteMetadata(desired)
	setEmptyStatus(desired)
	labels := desired.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[DownstreamManagedLabel] = r.SourceID
	desired.SetLabels(labels)

	key := client.ObjectKeyFromObject(source)
	current := kind.newObject()
	if err := r.Local.Get(ctx, key, current); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		return r.Local.Create(ctx, desired)
	}

	if current.GetLabels()[DownstreamManagedLabel] != r.SourceID {
		return fmt.Errorf("local object already exists and is not managed by this upstream")
	}
	if current.GetDeletionTimestamp() != nil {
		return nil
	}

	desired.SetResourceVersion(current.GetResourceVersion())
	desired.SetUID(current.GetUID())
	desired.SetGeneration(current.GetGeneration())
	desired.SetCreationTimestamp(current.GetCreationTimestamp())
	desired.SetOwnerReferences(current.GetOwnerReferences())
	desired.SetFinalizers(current.GetFinalizers())
	copyLocalStatus(desired, current)
	if reflect.DeepEqual(current, desired) {
		return nil
	}
	return r.Local.Update(ctx, desired)
}

func clearRemoteMetadata(object client.Object) {
	object.SetUID("")
	object.SetResourceVersion("")
	object.SetGeneration(0)
	object.SetCreationTimestamp(metav1.Time{})
	object.SetDeletionTimestamp(nil)
	object.SetDeletionGracePeriodSeconds(nil)
	object.SetManagedFields(nil)
	object.SetOwnerReferences(nil)
	object.SetFinalizers(nil)
}

func setEmptyStatus(object client.Object) {
	switch typed := object.(type) {
	case *infrastructurev1alpha1.Location:
		typed.Status = infrastructurev1alpha1.LocationStatus{}
	case *infrastructurev1alpha1.HealthCheckProfile:
		typed.Status = infrastructurev1alpha1.HealthCheckProfileStatus{}
	case *infrastructurev1alpha1.Zone:
		typed.Status = infrastructurev1alpha1.ZoneStatus{}
	case *infrastructurev1alpha1.PrefixList:
		typed.Status = infrastructurev1alpha1.PrefixListStatus{}
	case *infrastructurev1alpha1.DNSEndpoint:
		typed.Status = infrastructurev1alpha1.DNSEndpointStatus{}
	case *infrastructurev1alpha1.Service:
		typed.Status = infrastructurev1alpha1.ServiceStatus{}
	}
}

func copyLocalStatus(destination, current client.Object) {
	switch desired := destination.(type) {
	case *infrastructurev1alpha1.Location:
		desired.Status = current.(*infrastructurev1alpha1.Location).Status
	case *infrastructurev1alpha1.HealthCheckProfile:
		desired.Status = current.(*infrastructurev1alpha1.HealthCheckProfile).Status
	case *infrastructurev1alpha1.Zone:
		desired.Status = current.(*infrastructurev1alpha1.Zone).Status
	case *infrastructurev1alpha1.PrefixList:
		desired.Status = current.(*infrastructurev1alpha1.PrefixList).Status
	case *infrastructurev1alpha1.DNSEndpoint:
		desired.Status = current.(*infrastructurev1alpha1.DNSEndpoint).Status
	case *infrastructurev1alpha1.Service:
		desired.Status = current.(*infrastructurev1alpha1.Service).Status
	}
}

func (r *DownstreamReconciler) deleteMirror(ctx context.Context, kind downstreamResourceKind, key types.NamespacedName) error {
	current := kind.newObject()
	if err := r.Local.Get(ctx, key, current); err != nil {
		return client.IgnoreNotFound(err)
	}
	if current.GetLabels()[DownstreamManagedLabel] != r.SourceID {
		return nil
	}
	if err := r.Local.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

var _ interface {
	Start(context.Context) error
	NeedLeaderElection() bool
} = (*DownstreamReconciler)(nil)
