###Template-Operator Sample Data###

This directory contains sample data for the template-operator.
The directory is by default embedded in the template-operator docker image, so the operator can access the files placed here when deployed in the cluster.
You can use that to test how the operator works.

The `helm` subdirectory contains a sample Helm chart that installs a `redis` deployment.
You can install the chart by creating a `SampleHelm` CustomResource defined in the `<PROJECT_ROOT>/config/samples/operator.kyma-project.io_v1alpha1_samplehelm.yaml` file.

The `yaml` subdirectory contains a YAML manifest (multi-document YAML file) that also installs a `redis` deployment.
The installed objects correspond to the ones created by the `SampleHelm` CustomResource, but their names and the namespace is different, so you can install both.
You can install the manifest by creating a `Sample` CustomResource defined in the `<PROJECT_ROOT>/config/samples/operator.kyma-project.io_v1alpha1_sample.yaml` file.

If you want to install your own chart/manifest, you have two options:
1. Change **this** sample data and build your own custom docker image that you'll use in deployment: `make docker-build`
2. Deploy template-operator as-is, and reconfigure it's deployment to mount additional files into the operator Pod using Kubernetes volume mounts feature. Then refer to the mounted folder in the Sample/SampleHelm CustomResource you'll use to trigger the installation.

When running the controller locally with `make run`, it has the access to your local filesystem, so use local paths for Sample/SampleHelm CustomResources.

