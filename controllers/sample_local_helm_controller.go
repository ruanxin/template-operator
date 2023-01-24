/*
Copyright 2022.

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

package controllers

import (
	"bytes"
	"context"
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/kube"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyma-project/template-operator/api/v1alpha1"
)

var CustomNs = "helm-custom-ns"

// SampleHelmReconciler reconciles a SampleHelm object
type SampleHelmReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// EventRecorder for creating k8s events
	record.EventRecorder

	Config *rest.Config
}

//+kubebuilder:rbac:groups=operator.kyma-project.io.kyma-project.io,resources=samplehelms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.kyma-project.io.kyma-project.io,resources=samplehelms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.kyma-project.io.kyma-project.io,resources=samplehelms/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SampleHelm object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *SampleHelmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	objectInstance := v1alpha1.SampleHelm{}

	if err := r.Client.Get(ctx, req.NamespacedName, &objectInstance); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		logger.Info(req.NamespacedName.String() + " got deleted!")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	status := objectInstance.Status

	// check if deletionTimestamp is set, retry until it gets deleted
	// set state to Deleting if not set for an object with deletion timestamp
	if !objectInstance.GetDeletionTimestamp().IsZero() &&
		status.State != v1alpha1.StateDeleting {
		// if the status is not yet set to deleting, also update the status
		return ctrl.Result{}, r.setStatusForObjectInstance(ctx, &objectInstance, status.WithState(v1alpha1.StateDeleting))
	}

	// add finalizer if not present
	if controllerutil.AddFinalizer(&objectInstance, finalizer) {
		return ctrl.Result{}, r.ssa(ctx, &objectInstance)
	}

	switch status.State {
	case "":
		return ctrl.Result{}, r.HandleInitialState(ctx, &objectInstance)
	case v1alpha1.StateProcessing:
		return ctrl.Result{Requeue: true}, r.HandleProcessingState(ctx, &objectInstance)
	case v1alpha1.StateDeleting:
		return ctrl.Result{Requeue: true}, r.HandleDeletingState(ctx, &objectInstance)
	case v1alpha1.StateError:
		return ctrl.Result{Requeue: true}, r.HandleErrorState(ctx, &objectInstance)
	case v1alpha1.StateReady:
		return ctrl.Result{RequeueAfter: requeueInterval}, r.HandleReadyState(ctx, &objectInstance)
	}

	return ctrl.Result{}, nil
}

// HandleInitialState bootstraps state handling for the reconciled resource.
func (r *SampleHelmReconciler) HandleInitialState(ctx context.Context, objectInstance *v1alpha1.SampleHelm) error {
	status := objectInstance.Status

	return r.setStatusForObjectInstance(ctx, objectInstance, status.
		WithState(v1alpha1.StateProcessing).
		WithInstallConditionStatus(metav1.ConditionUnknown, objectInstance.GetGeneration()))
}

// HandleProcessingState processes the reconciled resource by processing the underlying resources.
// Based on the processing either a success or failure state is set on the reconciled resource.
func (r *SampleHelmReconciler) HandleProcessingState(ctx context.Context, objectInstance *v1alpha1.SampleHelm) error {
	status := objectInstance.Status
	if err := r.processResources(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ResourcesInstall", err.Error())
		return r.setStatusForObjectInstance(ctx, objectInstance, status.
			WithState(v1alpha1.StateError).
			WithInstallConditionStatus(metav1.ConditionFalse, objectInstance.GetGeneration()))
	}
	// set eventual state to Ready - if no errors were found
	return r.setStatusForObjectInstance(ctx, objectInstance, status.
		WithState(v1alpha1.StateReady).
		WithInstallConditionStatus(metav1.ConditionTrue, objectInstance.GetGeneration()))
}

// HandleErrorState handles error recovery for the reconciled resource.
func (r *SampleHelmReconciler) HandleErrorState(ctx context.Context, objectInstance *v1alpha1.SampleHelm) error {
	status := objectInstance.Status
	if err := r.processResources(ctx, objectInstance); err != nil {
		return err
	}
	// set eventual state to Ready - if no errors were found
	return r.setStatusForObjectInstance(ctx, objectInstance, status.
		WithState(v1alpha1.StateReady).
		WithInstallConditionStatus(metav1.ConditionTrue, objectInstance.GetGeneration()))
}

// HandleDeletingState processed the deletion on the reconciled resource.
// Once the deletion if processed the relevant finalizers (if applied) are removed.
func (r *SampleHelmReconciler) HandleDeletingState(ctx context.Context, objectInstance *v1alpha1.SampleHelm) error {
	r.Event(objectInstance, "Normal", "Deleting", "resource deleting")
	logger := log.FromContext(ctx)

	status := objectInstance.Status

	resources, err := r.render(ctx, objectInstance)
	if err != nil && controllerutil.RemoveFinalizer(objectInstance, finalizer) {
		// if error is encountered simply remove the finalizer and delete the reconciled resource
		return r.Client.Update(ctx, objectInstance)
	}
	r.Event(objectInstance, "Normal", "ResourcesDelete", "deleting resources")

	errGroup := NewErrorGrp()

	// instead of looping a concurrent mechanism can also be implemented
	for _, resource := range resources.Items {
		if err = r.Client.Delete(ctx, resource); err != nil && !errors2.IsNotFound(err) {
			errGroup.Add(err)
		}
	}

	if errGroup.Error() != nil {
		logger.Error(errGroup.Error(), "error during uninstallation of resources")
		r.Event(objectInstance, "Warning", "ResourcesDelete", "deleting resources error")
		return r.setStatusForObjectInstance(ctx, objectInstance, status.
			WithState(v1alpha1.StateError).
			WithInstallConditionStatus(metav1.ConditionFalse, objectInstance.GetGeneration()))
	}

	// if resources are ready to be deleted, remove finalizer
	if controllerutil.RemoveFinalizer(objectInstance, finalizer) {
		return r.Client.Update(ctx, objectInstance)
	}
	return nil
}

// HandleReadyState checks for the consistency of reconciled resource, by verifying the underlying resources.
func (r *SampleHelmReconciler) HandleReadyState(ctx context.Context, objectInstance *v1alpha1.SampleHelm) error {
	status := objectInstance.Status
	if err := r.processResources(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ResourcesInstall", err.Error())
		return r.setStatusForObjectInstance(ctx, objectInstance, status.
			WithState(v1alpha1.StateError).
			WithInstallConditionStatus(metav1.ConditionFalse, objectInstance.GetGeneration()))
	}
	return nil
}

func (r *SampleHelmReconciler) setStatusForObjectInstance(ctx context.Context, objectInstance *v1alpha1.SampleHelm,
	status *v1alpha1.SampleHelmStatus,
) error {
	objectInstance.Status = *status

	if err := r.ssaStatus(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ErrorUpdatingStatus", fmt.Sprintf("updating state to %v", string(status.State)))
		return fmt.Errorf("error while updating status %s to: %w", status.State, err)
	}

	r.Event(objectInstance, "Normal", "StatusUpdated", fmt.Sprintf("updating state to %v", string(status.State)))
	return nil
}

// ssaStatus patches status using SSA on the passed object
func (r *SampleHelmReconciler) ssaStatus(ctx context.Context, obj client.Object) error {
	obj.SetManagedFields(nil)
	obj.SetResourceVersion("")
	return r.Status().Patch(ctx, obj, client.Apply,
		&client.SubResourcePatchOptions{PatchOptions: client.PatchOptions{FieldManager: fieldOwner}})
}

// ssa patches the object using SSA
func (r *SampleHelmReconciler) ssa(ctx context.Context, obj client.Object) error {
	obj.SetManagedFields(nil)
	obj.SetResourceVersion("")
	return r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))
}

func (r *SampleHelmReconciler) render(ctx context.Context, obj *v1alpha1.SampleHelm) (*ManifestResources, error) {
	// create custom namespace resource
	ns := getNsResource(CustomNs)
	if err := r.ssa(ctx, ns); err != nil {
		return nil, err
	}

	logger := log.FromContext(ctx)
	restClientGetter := NewRESTClientGetter(r.Config)

	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(restClientGetter, CustomNs, "memory", func(format string, v ...interface{}) {
		format = fmt.Sprintf("[debug] %s\n", format)
		debugLevel := 2
		logger.V(debugLevel).Info(fmt.Sprintf(format, v...))
	}); err != nil {
		logger.Error(err, "")
	}

	// override custom namespace
	kubeClient := actionConfig.KubeClient.(*kube.Client)
	kubeClient.Namespace = CustomNs

	actionClient := action.NewInstall(actionConfig)

	// Helm flags
	r.setClientConfig(actionClient, "sample-release-name")

	// Helm values
	valuesAsMap := map[string]interface{}{
		"label": "custom-label-from-controller",
	}

	// load Helm chart from local path
	chrt, err := loader.Load(obj.Spec.ChartPath)
	if err != nil {
		return nil, err
	}

	// parse Helm chart resources
	release, err := actionClient.RunWithContext(ctx, chrt, valuesAsMap)
	if err != nil {
		return nil, err
	}

	resourceList, err := kubeClient.Build(bytes.NewBufferString(release.Manifest), true)
	if err != nil {
		return nil, err
	}

	resources := &ManifestResources{}
	for _, info := range resourceList {
		resources.Items = append(resources.Items, info.Object.(*unstructured.Unstructured))
	}

	return resources, nil
}

func (r *SampleHelmReconciler) setClientConfig(actionClient *action.Install, releaseName string) {
	actionClient.DryRun = true
	actionClient.Atomic = false
	actionClient.Wait = false
	actionClient.WaitForJobs = false
	actionClient.Replace = true     // Skip the name check
	actionClient.IncludeCRDs = true // include CRDs in the templated output
	actionClient.ClientOnly = false
	actionClient.ReleaseName = releaseName
	actionClient.Namespace = CustomNs
	actionClient.CreateNamespace = true
	actionClient.IsUpgrade = true

	// default versioning if unspecified
	if actionClient.Version == "" && actionClient.Devel {
		actionClient.Version = ">0.0.0-0"
	}
}

func (r *SampleHelmReconciler) processResources(ctx context.Context, objectInstance *v1alpha1.SampleHelm) error {
	resources, err := r.render(ctx, objectInstance)
	if err != nil {
		return err
	}

	errorGrp := NewErrorGrp()

	// instead of looping a concurrent mechanism can also be implemented
	for _, resource := range resources.Items {
		err = r.ssa(ctx, resource)
		if err != nil {
			errorGrp.Add(err)
		}
	}

	return errorGrp.Error()
}

// SetupWithManager sets up the controller with the Manager.
func (r *SampleHelmReconciler) SetupWithManager(mgr ctrl.Manager, rateLimiter RateLimiter) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.SampleHelm{}).
		WithOptions(controller.Options{
			RateLimiter: TemplateRateLimiter(
				rateLimiter.BaseDelay,
				rateLimiter.FailureMaxDelay,
				rateLimiter.Frequency,
				rateLimiter.Burst,
			),
		}).
		Complete(r)
}
