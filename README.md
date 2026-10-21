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

Create the namespace and Project before tenant resources. Namespaces are not automatically provisioned by the Project webhook. Every non-Project CRD, including Location, requires an immutable `spec.project: {name: <project-name>}`. References resolve within the resource's namespace and must belong to the same Project, including DNS zones, PrefixList destinations, Location parents/fallbacks, and healthcheck profiles. Create dependencies first; establish cyclic Location fallback relationships through updates after creating the Locations without those references.

Generated Service DNS endpoints inherit the Project and use a matching same-Project Zone. Infrastructure placement is still controlled separately by `--infrastructure-target-namespace`. Infrastructure caches are not restricted by tenant placement flags. Use the same placement configuration for all instances of one deployment; independent deployments must not install overlapping admission webhooks with conflicting policies.

Admission webhooks must be deployed, reachable, and enabled (`ENABLE_WEBHOOKS` must not be `false`) to enforce placement and reference checks. CRD schemas independently require `spec.project.name` and prevent ownership changes. The stock Kustomize deployment has webhook/certificate integration commented out; enable its WEBHOOK and CERTMANAGER sections, or deploy equivalent TLS and webhook configuration, before relying on tenant admission enforcement.

Namespace-per-project placement supports Kubernetes namespace RBAC isolation. Shared-namespace placement does not: enforce Project authorization in the API layer and restrict tenants' direct Kubernetes access. Project references alone do not grant or restrict access. Project deletion does not automatically delete tenant resources.

#### Breaking Migration

API keys now use `spec.project` instead of `spec.projectRef`. Before installing the stricter CRDs/webhooks, stage a permissive schema accepting the new field and backfill Project references on existing resources and manifests, including Locations. Move tenants into their intended namespaces and give Projects matching names in dedicated mode. Kubernetes namespaces and object names cannot be changed in place, so recreate resources when necessary. Update API clients and GitOps manifests, then install the generated strict CRDs and enable admission enforcement. Do not switch placement modes until existing resources satisfy the new placement policy.

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

