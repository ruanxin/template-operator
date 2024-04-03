# Enhanced Deployment Configuration Template for End-to-End Testing

This document outlines how to use arguments to imitate specific module behavior to support certain end-to-end (e2e) test scenarios.

- `final-state`

   This argument allows customization of the final state of a Module CR (`sample-yaml`). By default, a Module CR transitions to the `Ready` state. However, the final state can be configured to differ from this default.

   For instance, to override the final state to `Warning`, add `--final-state=Warning` as a deployment argument. In this case, once the Module CR is deployed, it remains in the `Warning` state.

- `final-deletion-state`

   This argument is used to customize the final state of a Module CR (`sample-yaml`) when the CR is flagged for deletion. The default state in this case is `Deleting`.

## Related End-to-End Tests:

- [Warning Status Propagation](https://github.com/kyma-project/lifecycle-manager/blob/a0c49436f3d11d03c9a7556ec11c7c9f69d621d9/tests/e2e/warning_status_propagation_test.go#L17) - in this test scenario the `final-state` and `final-deletion-state` arguments are used to set the Module CR's final state to `Warning`.