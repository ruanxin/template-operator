# Enhanced Deployment Configuration Template for End-to-End Testing

This document outlines the process of utilizing arguments to imitate specific module behavior to support certain end-to-end (e2e) test scenarios.

- `final-state`

This argument allows customization of the final state of the Module CR (`sample-yaml`). By default, the Module CR will transition to the `Ready` state. However, the final state can be configured to differ from this default.

For instance, the `final-state` can be overridden to `Warning` by adding `--final-state=Warning` as a deployment argument. In this case, once the Module CR is deployed, it will remain in the `Warning` state.

- `final-deletion-state`

This argument is used to customize the final state of the Module CR (`sample-yaml`) when the CR is flagged for deletion. The default state in this instance is `Deleting`.

## Related End-to-End Tests:

- [Warning Status Propagation](https://github.com/kyma-project/lifecycle-manager/blob/a0c49436f3d11d03c9a7556ec11c7c9f69d621d9/tests/e2e/warning_status_propagation_test.go#L17): This test is related to the scenario where the `final-state`,  `final-deletion-state` arguments are used to set the Module CR's final state to `Warning`.