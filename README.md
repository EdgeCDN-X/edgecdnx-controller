# edgecdnx-controller
// TODO(user): Add simple overview of use/purpose

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/edgecdnx-controller:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/edgecdnx-controller:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -f config/samples/infrastructure_v1alpha1_project.yaml
kubectl apply -n tenant-demo -f config/samples/infrastructure_v1alpha1_healthcheckprofile.yaml
kubectl apply -n tenant-demo -f config/samples/infrastructure_v1alpha1_location.yaml
kubectl apply -n tenant-demo -f config/samples/infrastructure_v1alpha1_zone.yaml
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

### Project Placement

Projects represent tenants. All EdgeCDN-X CRDs remain Kubernetes namespaced resources.

| Flags | Placement | Tenant CRD watches |
| --- | --- | --- |
| `--namespaced=true` (default) | One Project per namespace; its `metadata.name` must equal the namespace name | All namespaces |
| `--namespaced=false --target-namespace=edgecdnx` | Multiple Projects share the configured namespace | Only the target namespace |

Create the namespace and Project before tenant resources. Namespaces are not automatically provisioned by the Project webhook. Every non-Project CRD, including Location, requires an immutable `spec.project: {name: <project-name>}`. References belong to the same Project by default. PrefixList destinations, Location parents/fallbacks, and healthcheck profiles remain project-local. Zone references can cross namespaces/projects only through the sharing policy below. Create dependencies first; establish cyclic Location fallback relationships through updates after creating the Locations without those references.

Generated Service DNS endpoints inherit the Service's Project and use a matching owned or explicitly shared Zone. Infrastructure placement is still controlled separately by `--infrastructure-target-namespace`. Infrastructure caches are not restricted by tenant placement flags. Use the same placement configuration for all instances of one deployment; independent deployments must not install overlapping admission webhooks with conflicting policies.

Admission webhooks must be deployed, reachable, and enabled (`ENABLE_WEBHOOKS` must not be `false`) to enforce placement and reference checks. CRD schemas independently require `spec.project.name` and prevent ownership changes. The stock Kustomize deployment has webhook/certificate integration commented out; enable its WEBHOOK and CERTMANAGER sections, or deploy equivalent TLS and webhook configuration, before relying on tenant admission enforcement.

Namespace-per-project placement supports Kubernetes namespace RBAC isolation. Shared-namespace placement does not: enforce Project authorization in the API layer and restrict tenants' direct Kubernetes access. Project references alone do not grant or restrict access. Project deletion does not automatically delete tenant resources.

#### Shared platform infrastructure

Zones and Locations stay owned by a platform Project; sharing grants consumption,
never management rights. They are private when `spec.sharing` is absent. For example:

```yaml
apiVersion: infrastructure.edgecdnx.com/v1alpha1
kind: Zone
metadata:
  name: cdn
  namespace: platform
spec:
  project:
    name: platform
  zone: cdn.edgecdnx.com
  email: noc.edgecdnx.com
  sharing:
    projects:
      - namespace: customer
        name: customer
```

Locations accept the same `sharing` field. `sharing: {allProjects: true}` grants
all projects in this deployment consumption access. An omitted grant namespace
means the resource's namespace, useful when multiple projects share one namespace.
In shared-namespace mode, both platform and customer resources must be in the
configured target namespace.

The Service's primary domain must be a single-label hostname under the shared
zone, for example `unique-id.cdn.edgecdnx.com`. The API generates this hostname
using its configured service base domain; GitOps Services can specify it directly.
The Service reconciler runs only with `--role=controller` and the
`cachingreconciler` feature gate. Router-role instances do not watch or reconcile
Services; downstream router targets consume the generated DNSEndpoints.
The control-plane Service reconciler chooses the longest authorized matching zone and rejects
ambiguous matches. It atomically reserves shared hostnames using a non-expiring
coordination Lease in the zone's namespace, held by the Service UID. A Service
finalizer releases the reservation on deletion. Allocated domains are immutable;
create a replacement Service to migrate. Do not remove allocation finalizers or
edit reservation Leases manually.

Generated A/AAAA DNSEndpoints remain in the customer namespace/project, controlled
by their Service. Their zone reference is now a Kubernetes ObjectReference:

```yaml
zone:
  namespace: platform
  name: cdn
  # Optional: kind: Zone
  # Optional: apiVersion: infrastructure.edgecdnx.com/v1alpha1
```

Legacy `zone: {name: cdn}` manifests continue to resolve in the endpoint namespace.
If supplied, kind, apiVersion and UID must match the target Zone; fieldPath and
resourceVersion are not supported. Cross-project references require a shared
zone, a live owning Service, and its hostname reservation. Tenants cannot create
arbitrary TXT/CNAME/apex/wildcard records in shared zones or claim another Service's
hostname. DELEGATION host aliases require an owned matching zone and get A/AAAA
DNSEndpoints. CNAME aliases are externally managed and get no DNSEndpoints.
Legacy aliases without targetType get endpoints only when a live owned zone
matches; otherwise they are left externally managed. Changing an alias to CNAME
or removing it deletes its previously generated endpoints. The primary
`spec.domain` always gets A/AAAA endpoints in the Service's project, referencing
the selected owned or shared Zone by namespace/name; shared-zone reservations
are managed on the control plane, not by router instances.

Host aliases may specify `targetType: CNAME` or `targetType: DELEGATION`.
On Service creation and update, DELEGATION aliases must fall within a live
Zone owned by that same namespace and Project (the zone apex is also allowed).
A shared Zone owned by another Project does not qualify. CNAME aliases do not
require an owned Zone in Service admission. An omitted targetType retains
legacy behavior; no default is written to existing resources.

With the `cachingreconciler` feature gate enabled, the control-plane Service
controller creates a cert-manager Certificate for the primary domain and each
host alias that has no user-provided certificate or reference. The ClusterIssuer
is selected with `--cluster-issuer-name` (default `edgecdnx`). Certificates have
the `solver-target` label set to `cname` for CNAME aliases and `delegation` for
the primary domain, DELEGATION aliases, and legacy aliases without a targetType.
Configure the ClusterIssuer solvers accordingly: DNS-01 for `delegation` and
HTTP-01 for `cname`. HTTP-01 requires the CNAME to resolve to the CDN and
port 80 to reach every eligible cache.

When a Certificate is ready, its Secret's `tls.crt` and `tls.key` are base64
encoded into the corresponding Service certificate fields, together with the
managed certificate and secret references. Renewals update the Service so its
downstream cache mirror receives the new material. Existing user-supplied
certificate fields are preserved. To replace a managed certificate with a
user-supplied one, replace its certificate fields and clear the managed
`certificateRef`; set `secretRef` to the desired cache Secret name. Removing
an alias deletes its managed Certificate; cert-manager's configuration
determines whether its Secret is also deleted. The APISIX cache controller
creates a TLS Secret at the certificate's `secretRef` and an ApisixTls resource for each domain or alias
with inline certificate material. Inline `crt` and `key` must both be base64
encoded, match each other, cover the hostname, and include a valid `secretRef`.
The controller will not overwrite Secrets or ApisixTls resources it does not
manage. Changes and renewals update the resources; removing a certificate or
alias removes its generated resources. Ingress-nginx is deprecated.

For static-origin APISIX routes, a non-empty `spec.cors` enables the CORS
plugin. `allowOrigin` and `allowMethods` are comma-separated lists; omitted
values use APISIX defaults. With `allowCredentials: true`, both must be
specified and cannot be `*`; request headers use ingress-nginx's default
allowed-header list. APISIX also accepts `**` to force wildcard access with
credentials, but this carries security risks. Unlike ingress-nginx, APISIX
does not interpret wildcard hostnames in `allowOrigin` as patterns. An invalid
credential configuration fails reconciliation and leaves an existing route
unchanged until corrected.

For static-origin APISIX routes, `spec.path.rewrites` configures ordered
`proxy-rewrite` regex rules. The first matching pattern rewrites the upstream
path; capture groups can be referenced in the replacement:

```yaml
path:
  paths:
    - /images/*
  rewrites:
    - pattern: "^/images/(.*)$"
      replacement: "/assets/$1"
```

Route paths still determine which requests reach the route; rewrite patterns
are matched separately. The legacy `spec.path.rewrite` field is not applied
by APISIX.

DNS routing considers owned and explicitly shared Locations, then applies the
Service's label selector. Sharing does not bypass selectors: avoid a
customer-specific `project` label selector when intentionally using platform
Locations. Location hierarchy and healthcheck configuration stay platform-owned.
Deploy the updated DNS plugin together with these controller changes. The
existing Apache APISIX cache reconciler remains in place.

Zone sharing permission removal and deletion are blocked while dependent
Services or DNSEndpoints remain; migrate or delete those consumers first.
Location sharing revocation and deletion are not blocked by Service/DNSEndpoint
consumers, allowing operators to replace or retire location pools. Location
deletion still rejects local parent, fallback, and PrefixList references.
Location ownership, sharing-policy format, base-domain Zone membership, and
healthcheck/reference validation remain enforced on create and update. Removing
a Location or its sharing can remove routing capacity; provision replacements
beforehand when uninterrupted service is required. Existing
ownership-scoped API CRUD remains unchanged. The API exposes a safe consumption
catalog at `GET /project/:id/available-infrastructure`; the UI displays shared
resources read-only. Administrators can manage grants via
`PATCH /admin/zones/:namespace/:name/sharing` and
`PATCH /admin/locations/:namespace/:name/sharing`, or use GitOps. PATCH requests
include `allProjects`, `projects` and the catalog object's `resourceVersion`;
stale edits return a conflict rather than overwriting another administrator's grants.

Install the regenerated controller and Helm CRDs before applying these fields,
and deploy the updated webhook configuration (including DELETE validation).
Keep platform namespace access, sharing policies and hostname reservation Leases
restricted to trusted administrators/controller identities. In shared-namespace
mode, tenants must use the authorized API rather than direct Kubernetes writes.

#### Breaking Migration

API keys now use `spec.project` instead of `spec.projectRef`. Before installing the stricter CRDs/webhooks, stage a permissive schema accepting the new field and backfill Project references on existing resources and manifests, including Locations. Move tenants into their intended namespaces and give Projects matching names in dedicated mode. Kubernetes namespaces and object names cannot be changed in place, so recreate resources when necessary. Update API clients and GitOps manifests, then install the generated strict CRDs and enable admission enforcement. Do not switch placement modes until existing resources satisfy the new placement policy.

### Healthcheck Thresholds

HealthCheckProfile accepts optional `spec.successThreshold` and
`spec.failureThreshold` integers, each with a minimum and default of `1`.
Existing profiles and manifests omitting these fields retain one-result
transitions; no profile backfill is required.

The settings apply independently to every probe and IP stack. A transition
requires consecutive successes or failures; the opposite result resets the
streak. Probes start unhealthy and require `successThreshold` successes to
become healthy. Counters stay in healthchecker memory, are never written to
Kubernetes, and reset on process restart or profile/location configuration changes.
Install the updated CRD before configuring thresholds through the API.

### Downstream Mode

Run one downstream instance per upstream cluster and target type. The upstream kubeconfig must provide `get`, `list`, and `watch` access to the target CRDs. Upstream informers deliver changes reactively; a paginated sweep of only this source's local mirrors runs at startup to recover deletions missed while the downstream process was offline. The downstream instance uses its local service account to create, update, and delete mirrored resources and create missing namespaces. Upstream status and Kubernetes-managed fields are not copied.

For Helm deployments, store the upstream kubeconfig in a Secret in the release namespace. By default, the chart reads the `kubeconfig` key:

```yaml
role: downstream
webhook:
    enabled: "false"
downstream:
    target: router # router or cache
    kubeconfigSecretName: upstream-cluster-kubeconfig
    kubeconfigSecretKey: kubeconfig
    workers: 16
    clientQPS: 100
    clientBurst: 200
```

The standalone binary accepts the same configuration through `--role=downstream`, `--target=router|cache`, and `--upstream-kubeconfig=/path/to/kubeconfig`.

For APISIX caches, set `downstream.http01SolverImage` to a cert-manager
`acmesolver` image (or pass `--downstream-http01-solver-image`). The downstream
controller watches upstream HTTP-01 Challenges owned by managed CNAME
certificates and creates a local solver Pod, Service, and exact-match
`ApisixRoute`. It removes them when the Challenge completes or disappears and
recovers missed deletions after a restart. The upstream identity needs read
access to Challenges, Orders, Certificates, and CertificateRequests; the local
identity needs access to Pods, Services, and ApisixRoutes. APISIX global plugins
and redirects must allow the challenge path without authentication or rewriting.

Propagation is asynchronous: cert-manager's self-check does not establish that
all caches are ready. ACME validation may fail and need a retry if it reaches
a cache before its route is installed. The issuer still creates its standard
temporary solver resources in the control cluster.

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/edgecdnx-controller:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/edgecdnx-controller/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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
