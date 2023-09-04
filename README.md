# Template Operator
This documentation and template serves as a reference to implement a module (component) operator, for integration with the [lifecycle-manager](https://github.com/kyma-project/lifecycle-manager/tree/main/).
It utilizes the [kubebuilder](https://book.kubebuilder.io/) framework with some modifications to implement Kubernetes APIs for custom resource definitions (CRDs).
Additionally, it hides Kubernetes boilerplate code to develop fast and efficient control loops in Go.


## Contents
* [Understanding module development in Kyma](#understanding-module-development-in-kyma)
* [Implementation](#implementation)
  * [Pre-requisites](#pre-requisites)
  * [Generate kubebuilder operator](#generate-kubebuilder-operator)
  * [Local testing](#local-testing)
* [Bundling and installation](#bundling-and-installation)
  * [Grafana dashboard for simplified Controller Observability](#grafana-dashboard-for-simplified-controller-observability)
  * [RBAC](#rbac)
  * [Build module operator image](#prepare-and-build-module-operator-image)
  * [Build and push your module to the registry](#build-and-push-your-module-to-the-registry)
* [Using your module in the Lifecycle Manager ecosystem](#using-your-module-in-the-lifecycle-manager-ecosystem)
  * [Deploying Kyma infrastructure operators with `kyma alpha deploy`](#deploying-kyma-infrastructure-operators-with-kyma-alpha-deploy)
  * [Deploying a `ModuleTemplate` into the Control Plane](#deploying-a-moduletemplate-into-the-control-plane)
  * [Debugging the operator ecosystem](#debugging-the-operator-ecosystem)
  * [Registering your module within the Control Plane](#registering-your-module-within-the-control-plane)

## Understanding module development in Kyma 

Before going in-depth, make sure you are familiar with:

- [Modularization in Kyma](https://github.com/kyma-project/community/tree/main/concepts/modularization)
- [Operator Pattern in Kubernetes](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

This Guide serves as comprehensive Step-By-Step tutorial on how to properly create a module from scratch by using an operator that is installing k8s yaml resources. 
Note that while other approaches are encouraged, there is no dedicated guide available yet and these will follow with sufficient requests and adoption of Kyma modularization.

Every Kyma Module using an Operator follows 5 basic Principles:

- Declared as available for use in a release channel through the `ModuleTemplate` Custom Resource in the control-plane
- Declared as desired state within the `Kyma` Custom Resource in runtime or control-plane
- Installed / managed in the runtime by [Lifecycle Manager](https://github.com/kyma-project/lifecycle-manager) through a `Manifest` custom resource in the control-plane
- Owns at least 1 Custom Resource Definition that defines the contract towards a Runtime Administrator and configures its behaviour
- Is operating on at most 1 runtime at every given time

Release channels let customers try new modules and features early, and decide when the updates should be applied. For more info, see the [release channels documentation in our Modularization overview](https://github.com/kyma-project/community/tree/main/concepts/modularization#release-channels).

The channel name has the following rules:
1. Lower case letters from a to z.
2. The total length is between 3 and 32.

In case you are planning to migrate a pre-existing module within Kyma, please familiarize yourself with the [transition plan for existing modules](https://github.com/kyma-project/community/blob/main/concepts/modularization/transition.md)

### Comparison to other established Frameworks

#### Operator Lifecycle Manager (OLM)

Compared to [OLM](https://olm.operatorframework.io/), the Kyma Modularization is similar, but distinct in a few key aspects.
While OLM is built heavily around a static dependency expression, Kyma Modules are expected to resolve dependencies dynamically.

Concretely, this means that while in OLM a Module has to declare CRDs and APIs that it depends on, in Kyma, all modules have the ability to depend on each other without declaring it in advance.
This makes it of course harder to understand compared to a strict dependency graph, but it comes with a few key advantages:

- Concurrent optimisation on controller level: every controller in Kyma is installed simultaneously and is not blocked from installation until other operators are available.
  This makes it easy to e.g. create or configure resources that do not need to wait for the dependency (e.g. a ConfigMap can be created even before a deployment that has to wait for an API to be present).
  While this enforces controllers to think about a case where the dependency is not present, we encourage Eventual Consistency and do not enforce a strict lifecycle model on our modules
- Discoverability is handled not through a registry / server, but through a declarative configuration.
  Every Module is installed through the `ModuleTemplate`, which is semantically the same as registering an operator in an OLM registry or `CatalogSource`. 
  The ModuleTemplate however is a normal CR and can be installed into a Control-Plane dynamically and with GitOps practices. 
  This allows multiple control-planes to offer differing modules simply at configuration time.
  Also, we do not use File-Based Catalogs for maintaining our catalog, but maintain every `ModuleTemplate` through [Open Component Model](https://ocm.software/), an open standard to describe software artifact delivery.

Regarding release channels for operators, Lifecycle Manager operates at the same level as OLM. However, with `Kyma` we ensure bundling of the `ModuleTemplate` to a specific release channel.
We are heavily inspired by the way that OLM handles release channels, but we do not have an intermediary `Subscription` that assigns the catalog to the channel. Instead, every module is deliverd in a `ModuleTemplate` in a channel already.

There is a distinct difference in parts of the `ModuleTemplate`. 
The ModuleTemplate contains not only a specification of the operator to be installed through a dedicated Layer.
It also consists of a set of default values for a given channel when installed for the first time.
When installing an operator from scratch through Kyma, this means that the Module will already be initialized with a default set of values.
However, when upgrading it is not expected from the Kyma Lifecycle to update the values to eventual new defaults. Instead it is a way for module developers to prefill their Operator with instructions based on a given environment (the channel).
It is important to note that these default values are static once they are installed, and they will not be updated unless a new installation of the module occurs, even when the content of `ModuleTemplate` changes. 
This is because a customer is expected to be able to change the settings of the module CustomResource at any time without the Kyma ecosystem overriding it.
Thus, the CustomResource of a Module can also be treated as a customer/runtime-facing API that allows us to offer typed configuration for multiple parts of Kyma.

### Crossplane

With [Crossplane](https://crossplane.io/), you are fundamentally allowing Providers to interact in your control-plane.
When looking at the Crossplane Lifecycle, the most similar aspect is that we also use opinionated OCI Images to bundle our Modules. 
We use the `ModuleTemplate` to reference our layers containing the necessary metadata to deploy our controllers, just like Crossplane.
However, we do not opinionate on Permissions of controllers and enforce stricter versioning guarantees, only allowing `semver` to be used for modules, and `Digest` for `sha` digests for individual layers of modules.

Fundamentally different is also the way that `Providers` and `Composite Resources` work compared to the Kyma ecosystem.
While Kyma allows any module to bring an individual CustomResource into the cluster for configuration, a `Provider` in Crossplane is located in the control-plane and only directs installation targets.
We handle this kind of data centrally through acquisition-strategies for credentials and other centrally managed data in the `Kyma` Custom Resource. 
Thus, it is most fitting, to consider the Kyma eco-system as a heavily opinionated `Composite Resource` from Crossplane, with the `Managed Resource` being tracked with the Lifecycle Manager `Manifest`. 

Compared to Crossplane, we also encourage the creation of own CustomResourceDefinitions in place of the concept of the `Managed Resource`, and in the case of configuration, we are able to synchronize not only a desired state for all modules from the control-plane, but also from the runtime.
Similarly, we make the runtime module catalog discoverable from inside the runtime with a dedicated synchronization mechanism.

Lastly, compared to Crossplane, we do not have as many choices when it comes to revision management and dependency resolution.
While in Crossplane, it is possible to define custom Package, Revision and Dependency Policies. 
However, in Kyma we opinionated here, since managed use-cases usually require unified revision handling, and we do not target a generic solution for revision management of different module eco-systems.

## Implementation

### Pre-requisites

* A provisioned Kubernetes Cluster and OCI Registry

  _WARNING: For all use cases in the guide, you will need a cluster for end-to-end testing outside your [envtest](https://book.kubebuilder.io/reference/envtest.html) integration test suite.
  This guide is HIGHLY RECOMMENDED to be followed for a smooth development process.
  This is a good alternative if you do not want to use an entire control-plane infrastructure and still want to properly test your operators.__
* [kubectl](https://kubernetes.io/docs/tasks/tools/)
* [kubebuilder](https://book.kubebuilder.io/)
    ```bash
    # you could use one of the following options
    
    # option 1: using brew
    brew install kubebuilder
    
    # option 2: fetch sources directly
    curl -L -o kubebuilder https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)
    chmod +x kubebuilder && mv kubebuilder /usr/local/bin/
    ```
* [kyma CLI](https://storage.googleapis.com/kyma-cli-stable/kyma-darwin)
* An OCI Registry to host OCI Image
  * Follow our [Provision cluster and OCI registry](https://github.com/kyma-project/lifecycle-manager/blob/main/docs/developer/provision-cluster-and-registry.md) guide to create a local registry provided by k3d
  * Or using [Google Container Registry (GCR)](https://github.com/kyma-project/lifecycle-manager/blob/main/docs/developer/prepare-gcr-registry.md) guide for a remote registry.
### Generate kubebuilder operator

1. Initialize `kubebuilder` project. Please make sure domain is set to `kyma-project.io`, the following command should execute in `test-operator` folder.
    ```shell
    kubebuilder init --domain kyma-project.io --repo github.com/kyma-project/test-operator --project-name=test-operator --plugins=go/v4-alpha
    ```

2. Create API group version and kind for the intended custom resource(s). Please make sure the `group` is set as `operator`.
    ```shell
    kubebuilder create api --group operator --version v1alpha1 --kind Sample --resource --controller --make
    ```

3. Run `make manifests`, to generate CRDs respectively.

A basic kubebuilder operator with appropriate scaffolding should be setup.

#### Optional: Adjust default config resources
If the module operator will be deployed under same namespace with other operators, differentiate your resources by adding common labels.

1. Add `commonLabels` to default `kustomization.yaml`, [reference implementation](config/default/kustomization.yaml).

2. Include all resources (e.g: [manager.yaml](config/manager/manager.yaml)) which contain label selectors by using `commonLabels`.

Further reading: [Kustomize built-in commonLabels](https://github.com/kubernetes-sigs/kustomize/blob/master/api/konfig/builtinpluginconsts/commonlabels.go)

#### Steps API definition

1. Refer to [State requirements](api/v1alpha1/status.go) and include them in your `Status` sub-resource similarly.

   This `Status` sub-resource should contain all valid `State` values (`.status.state`) values in order to be compliant with the Kyma ecosystem.
    ```go
    package v1alpha1
    // Status defines the observed state of Module CR.
    type Status struct {
        // State signifies current state of Module CR.
        // Value can be one of ("Ready", "Processing", "Error", "Deleting").
        // +kubebuilder:validation:Required
        // +kubebuilder:validation:Enum=Processing;Deleting;Ready;Error
        State State `json:"state"`
    }
    ```

    Include the `State` values in your `Status` sub-resource, either through inline reference or direct inclusion. These values have literal meaning behind them, so use them appropriately.

2. Optionally, you can add additional fields to your `Status` sub-resource. 
3. For instance, `Conditions` are added to `SampleCR` in the [API definition](api/v1alpha1/sample_types.go).
This also includes the required `State` values, using an inline reference.

    <details>
    <summary><b>Reference implementation SampleCR</b></summary>
    
    ```go
    package v1alpha1
    // Sample is the Schema for the samples API
    type Sample struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`
    
        Spec   SampleSpec   `json:"spec,omitempty"`
        Status SampleStatus `json:"status,omitempty"`
    }
    
    type SampleStatus struct {
        Status `json:",inline"`
    
        // Conditions contain a set of conditionals to determine the State of Status.
        // If all Conditions are met, State is expected to be in StateReady.
        Conditions []metav1.Condition `json:"conditions,omitempty"`
    
        // add other fields to status subresource here
    }
    ```
    </details>

4. Run `make generate manifests`, to generate boilerplate code and manifests.

#### Steps controller implementation

_Warning_: This sample implementation is only for reference. You could copy parts of implementation but please do not add this repository as a dependency to your project.

1. Implement `State` handling to represent the corresponding state of the reconciled resource, by following [kubebuilder](https://book.kubebuilder.io/) guidelines to implement controllers.

2. You could refer either to `SampleCR` [controller implementation](controllers/sample_controller_rendered_resources.go) for setting appropriate `State` and `Conditions` values to your `Status` sub-resource.

    `SampleCR` is reconciled to install / uninstall a list of rendered resources from a YAML file on the file system.
    
    ````go   
    r.setStatusForObjectInstance(ctx, objectInstance, status.
    WithState(v1alpha1.StateReady).
    WithInstallConditionStatus(metav1.ConditionTrue, objectInstance.GetGeneration()))
    ````
    
3. The reference controller implementations listed above use [Server-side apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) instead of conventional methods to process resources on the target cluster.
    Parts of this logic could be leveraged to implement your own controller logic. Checkout functions inside these controllers for state management and other implementation details.

### Local testing
* Connect to your cluster and ensure `kubectl` is pointing to the desired cluster.
* Install CRDs with `make install`
  _WARNING: This installs a CRD on your cluster, so create your cluster before running the `install` command. See [Pre-requisites](#pre-requisites) for details on the cluster setup._
* _Local setup_: install your module CR on a cluster and execute `make run` to start your operator locally.

_WARNING: Note that while `make run` fully runs your controller against the cluster, it is not feasible to compare it to a productive operator.
This is mainly because it runs with a client configured with privileges derived from your `KUBECONFIG` environment variable. For in-cluster configuration, see our [Guide on RBAC Management](#rbac)._

## Bundling and installation

### Grafana dashboard for simplified Controller Observability

You can extend the operator further by using automated dashboard generation for grafana.

By the following command, two grafana dashboard files with controller related metrics will be generated under `/grafana` folder.

```shell
kubebuilder edit --plugins grafana.kubebuilder.io/v1-alpha
```

To import the grafana dashboard, please read [official grafana guide](https://grafana.com/docs/grafana/latest/dashboards/export-import/#import-dashboard).
This feature is supported by [kubebuilder grafana plugin](https://book.kubebuilder.io/plugins/grafana-v1-alpha.html).

### RBAC
Make sure you have appropriate authorizations assigned to you controller binary, before you run it inside a cluster (not locally with `make run`).
The Sample CR [controller implementation](controllers/sample_controller.go) includes rbac generation (via kubebuilder) for all resources across all API groups.
This should be adjusted according to the chart manifest resources and reconciliation types.

Towards the earlier stages of your operator development RBACs could simply accommodate all resource types and adjusted later, as per your requirements.

```go
package controllers
// TODO: dynamically create RBACs! Remove line below.
//+kubebuilder:rbac:groups="*",resources="*",verbs="*"
```

_WARNING: Do not forget to run `make manifests` after this adjustment for it to take effect!_

### Prepare and build module operator image

_WARNING: This step requires the working OCI Registry from our [Pre-requisites](#pre-requisites)_

1. Include the static module data in your _Dockerfile_:
    ```dockerfile
    FROM gcr.io/distroless/static:nonroot
    WORKDIR /
    COPY module-data/ module-data/
    COPY --from=builder /workspace/manager .
    USER 65532:65532
    
    ENTRYPOINT ["/manager"]
    ``` 

    The sample module data in this repository includes a YAML manifest in `module-data/yaml` directories.
    You reference the YAML manifest directory with `spec.resourceFilePath` attribute of the `Sample` CR.
    The example custom resources in the `config/samples` directory are already referencing the mentioned directories.
    Feel free to organize the static data in a different way, the included `module-data` directory serves just as an example.
    You may also decide to not include any static data at all - in that case you have to provide the controller with the YAML data at runtime using other techniques, for example Kubernetes volume mounting.

2. Build and push your module operator binary by adjusting `IMG` if necessary and running the inbuilt kubebuilder commands.
   Assuming your operator image has the following base settings:
   * hosted at `op-kcp-registry.localhost:8888/unsigned/operator-images` 
   * controller image name is `sample-operator`
   * controller image has version `0.0.1`

   You can run the following command
    ```sh
    make docker-build docker-push IMG="op-kcp-registry.localhost:8888/unsigned/operator-images/sample-operator:0.0.1"
    ```
   
This will build the controller image and then push it as the image defined in `IMG` based on the kubebuilder targets.

### Build and push your module to the registry

_WARNING: This step requires the working OCI Registry, Cluster and Kyma CLI from our [Pre-requisites](#pre-requisites)_

1. The module operator manifests from the `default` kustomization (not the controller image) will now be bundled and pushed.
   Assuming the settings from [Prepare and build module operator image](#prepare-and-build-module-operator-image) for single-cluster mode, and assuming the following module settings:
   * hosted at `op-kcp-registry.localhost:8888/unsigned`
   * generated for channel `regular` (or any other name follow the channel naming rules)
   * module has version `0.0.1`
   * module name is `template`
   * for a k3d registry enable the `insecure` flag (`http` instead of `https` for registry communication)
   * uses Kyma CLI in `$PATH` under `kyma`
   * the default sample under `config/samples/operator_v1alpha1_sample.yaml` has been adjusted to be a valid CR by setting the default generated `Foo` field instead of a TODO.

     ```yaml
     apiVersion: operator.kyma-project.io/v1alpha1
     kind: Sample
     metadata:
       name: sample-sample
     spec:
       foo: bar
     ```
     _WARNING: The settings above reflect your default configuration for a module. If you want to change this you will have to manually adjust it to a different configuration. 
     You can also define multiple files in `config/samples`, however you will need to then specify the correct file during bundling._
   * The `.gitignore` has been adjusted and following ignores were added

     ```gitignore
     # kyma module cache
       mod
     # generated dummy charts
       charts
     # kyma generated by scripts or local testing
       kyma.yaml
     # template generated by kyma create module
       template.yaml
     ```

   Now, run the following command to create and push your module operator image to the specified registry:

   ```sh
   kyma alpha create module --version 0.0.1 --insecure --registry op-kcp-registry.localhost:8888/unsigned
   ```
   
   _WARNING: For external registries (e.g. Google Container/Artifact Registry), never use insecure. Instead, specify credentials. More details can be found in the help documentation of the CLI_

   To make a setup work in single-cluster mode adjust the generated `template.yaml` to install the module in the Control Plane, by assigning the field `.spec.target` to value `control-plane`. This will install all operators and modules in the same cluster.

   ```yaml
   apiVersion: operator.kyma-project.io/v1alpha1
   kind: ModuleTemplate
   #...
   spec:
     target: control-plane
   ```

2. Verify that the module creation succeeded and observe the `mod` folder. It will contain a `component-descriptor.yaml` with a definition of local layers.
    
    <details>
        <summary>Sample</summary>    

    ```yaml
    component:
      componentReferences: []
      name: kyma-project.io/module/sample
      provider: internal
      repositoryContexts:
      - baseUrl: op-kcp-registry.localhost:8888/unsigned
        componentNameMapping: urlPath
        type: ociRegistry
      resources:
      - access:
          digest: sha256:5982e2d0f7c49a8af684c4aa5b9713d639601e14dcce536e270848962892661b
          type: localOciBlob
        name: raw-manifest
        relation: local
        type: yaml
        version: 0.0.1
      sources: []
      version: 0.0.1
    meta:
      schemaVersion: v2
    ```
   
    As you can see the CLI created various layers that are referenced in the `blobs` directory. For more information on layer structure please reference the module creation with `kyma alpha mod create --help`.

    </details>

## Using your module in the Lifecycle Manager ecosystem

### Deploying Kyma infrastructure operators with `kyma alpha deploy`

_WARNING: This step requires the working OCI Registry and Cluster from our [Pre-requisites](#pre-requisites)_

Now that everything is prepared in a cluster of your choice, you are free to reference the module within any `Kyma` custom resource in your Control Plane cluster.

Deploy the [Lifecycle Manager](https://github.com/kyma-project/lifecycle-manager/tree/main) to the Control Plane cluster with:

```shell
kyma alpha deploy
```

### Deploying a `ModuleTemplate` into the Control Plane

Now run the command for creating the `ModuleTemplate` in the cluster.
After this the module will be available for consumption based on the module name configured with the label `operator.kyma-project.io/module-name` on the `ModuleTemplate`.

_WARNING: Depending on your setup against either a k3d cluster/registry, you will need to run the script in `hack/local-template.sh` before pushing the ModuleTemplate to have proper registry setup.
(This is necessary for k3d clusters due to port-mapping issues in the cluster that the operators cannot reuse, please take a look at the [relevant issue for more details](https://github.com/kyma-project/module-manager/issues/136#issuecomment-1279542587))_

```sh
kubectl apply -f template.yaml
```

For single-cluster mode, you could use the existing Kyma custom resource generated for the Control Plane in `kyma.yaml` with this:

```shell
kubectl patch kyma default-kyma -n kcp-system --type='json' -p='[{"op": "add", "path": "/spec/modules", "value": [{"name": "sample" }] }]'
```

This adds your module into `.spec.modules` with a name originally based on the `"operator.kyma-project.io/module-name": "sample"` label that was generated in `template.yaml`:

```yaml
spec:
  modules:
  - name: sample
```

If required, you can adjust this Kyma CR based on your testing scenario. For example, if you are running a dual-cluster setup, you might want to enable the synchronization of the Kyma CR into the runtime cluster for a full E2E setup.
On creation of this kyma CR in your Control Plane cluster, installation of the specified modules should start immediately.

### Debugging the operator ecosystem

The operator ecosystem around Kyma is complex, and it might become troublesome to debug issues in case your module is not installed correctly.
For this very reason here are some best practices on how to debug modules developed through this guide.

1. Verify the Kyma installation state is ready by verifying all conditions.
   ```shell
    JSONPATH='{range .items[*]}{@.metadata.name}:{range @.status.conditions[*]}{@.type}={@.reason}:{@.status};{end}{end}' \
    && kubectl get kyma -o jsonpath="$JSONPATH" -n kcp-system
   ```
2. Verify the Manifest installation state is ready by verifying all conditions.
   ```shell
    JSONPATH='{range .items[*]}{@.metadata.name}:{range @.status.conditions[*]}{@.type}={@.status};{end}{end}' \
    && kubectl get manifest -o jsonpath="$JSONPATH"-n kcp-system
   ```
3. Depending on your issue, either observe the deployment logs from either `lifecycle-manager` and/or `module-manager`. Make sure that no errors have occurred.

Usually the issue is related to either RBAC configuration (for troubleshooting minimum privileges for the controllers, see our dedicated [RBAC](#rbac) section), mis-configured image, module registry or `ModuleTemplate`.
As a last resort, make sure that you are aware if you are running within a single-cluster or a dual-cluster setup, watch out for any steps with a `WARNING` specified and retry with a freshly provisioned cluster.
For cluster provisioning, please make sure to follow our recommendations for clusters mentioned in our [Pre-requisites](#pre-requisites) for this guide.

Lastly, if you are still unsure, please feel free to open an issue, with a description and steps to reproduce, and we will be happy to help you with a solution.

### Registering your module within the Control Plane

For global usage of your module, the generated `template.yaml` from [Build and push your module to the registry](#build-and-push-your-module-to-the-registry) needs to be registered in our control-plane.
This relates to [Phase 2 of the Module Transition Plane](https://github.com/kyma-project/community/blob/main/concepts/modularization/transition.md#phase-2---first-module-managed-by-kyma-operator-integrated-with-keb). Please be patient until we can provide you with a stable guide on how to properly integrate your template.yaml
with an automated test flow into the central control-plane Offering.
