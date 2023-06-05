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
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyma-project/template-operator/api/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/controller"
)

// SampleReconciler reconciles a Sample object.
type SampleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	*rest.Config
	// EventRecorder for creating k8s events
	record.EventRecorder
	FinalState v1alpha1.State
}

type ManifestResources struct {
	Items []*unstructured.Unstructured
	Blobs [][]byte
}

//+kubebuilder:rbac:groups=operator.kyma-project.io,resources=samples,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.kyma-project.io,resources=samples/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.kyma-project.io,resources=samples/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete

// TODO: dynamically create RBACs! Remove line below.
//+kubebuilder:rbac:groups="*",resources="*",verbs="*"

// SetupWithManager sets up the controller with the Manager.
func (r *SampleReconciler) SetupWithManager(mgr ctrl.Manager, rateLimiter RateLimiter) error {
	r.Config = mgr.GetConfig()

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Sample{}).
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

// Reconcile is the entry point from the controller-runtime framework.
// It performs a reconciliation based on the passed ctrl.Request object.
func (r *SampleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	objectInstance := v1alpha1.Sample{}

	if err := r.Client.Get(ctx, req.NamespacedName, &objectInstance); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		logger.Info(req.NamespacedName.String() + " got deleted!")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// check if deletionTimestamp is set, retry until it gets deleted
	status := getStatusFromSample(&objectInstance)

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
	case v1alpha1.StateReady, v1alpha1.StateWarning:
		return ctrl.Result{RequeueAfter: requeueInterval}, r.HandleReadyState(ctx, &objectInstance)
	}

	return ctrl.Result{}, nil
}

// HandleInitialState bootstraps state handling for the reconciled resource.
func (r *SampleReconciler) HandleInitialState(ctx context.Context, objectInstance *v1alpha1.Sample) error {
	status := getStatusFromSample(objectInstance)

	return r.setStatusForObjectInstance(ctx, objectInstance, status.
		WithState(v1alpha1.StateProcessing).
		WithInstallConditionStatus(metav1.ConditionUnknown, objectInstance.GetGeneration()))
}

// HandleProcessingState processes the reconciled resource by processing the underlying resources.
// Based on the processing either a success or failure state is set on the reconciled resource.
func (r *SampleReconciler) HandleProcessingState(ctx context.Context, objectInstance *v1alpha1.Sample) error {
	status := getStatusFromSample(objectInstance)
	if err := r.processResources(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ResourcesInstall", err.Error())
		return r.setStatusForObjectInstance(ctx, objectInstance, status.
			WithState(v1alpha1.StateError).
			WithInstallConditionStatus(metav1.ConditionFalse, objectInstance.GetGeneration()))
	}
	// set eventual state to Ready - if no errors were found
	return r.setStatusForObjectInstance(ctx, objectInstance, status.
		WithState(r.FinalState).
		WithInstallConditionStatus(metav1.ConditionTrue, objectInstance.GetGeneration()))
}

// HandleErrorState handles error recovery for the reconciled resource.
func (r *SampleReconciler) HandleErrorState(ctx context.Context, objectInstance *v1alpha1.Sample) error {
	status := getStatusFromSample(objectInstance)
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
func (r *SampleReconciler) HandleDeletingState(ctx context.Context, objectInstance *v1alpha1.Sample) error {
	r.Event(objectInstance, "Normal", "Deleting", "resource deleting")
	logger := log.FromContext(ctx)

	status := getStatusFromSample(objectInstance)

	resourceObjs, err := getResourcesFromLocalPath(objectInstance.Spec.ResourceFilePath, logger)
	if err != nil && controllerutil.RemoveFinalizer(objectInstance, finalizer) {
		// if error is encountered simply remove the finalizer and delete the reconciled resource
		return r.Client.Update(ctx, objectInstance)
	}
	r.Event(objectInstance, "Normal", "ResourcesDelete", "deleting resources")

	// the resources to be installed are unstructured,
	// so please make sure the types are available on the target cluster
	for _, obj := range resourceObjs.Items {
		if err = r.Client.Delete(ctx, obj); err != nil && !errors2.IsNotFound(err) {
			logger.Error(err, "error during uninstallation of resources")
			r.Event(objectInstance, "Warning", "ResourcesDelete", "deleting resources error")
			return r.setStatusForObjectInstance(ctx, objectInstance, status.
				WithState(v1alpha1.StateError).
				WithInstallConditionStatus(metav1.ConditionFalse, objectInstance.GetGeneration()))
		}
	}

	// if resources are ready to be deleted, remove finalizer
	if controllerutil.RemoveFinalizer(objectInstance, finalizer) {
		return r.Client.Update(ctx, objectInstance)
	}
	return nil
}

// HandleReadyState checks for the consistency of reconciled resource, by verifying the underlying resources.
func (r *SampleReconciler) HandleReadyState(ctx context.Context, objectInstance *v1alpha1.Sample) error {
	status := getStatusFromSample(objectInstance)
	if err := r.processResources(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ResourcesInstall", err.Error())
		return r.setStatusForObjectInstance(ctx, objectInstance, status.
			WithState(v1alpha1.StateError).
			WithInstallConditionStatus(metav1.ConditionFalse, objectInstance.GetGeneration()))
	}
	return nil
}

func (r *SampleReconciler) setStatusForObjectInstance(ctx context.Context, objectInstance *v1alpha1.Sample,
	status *v1alpha1.SampleStatus,
) error {
	objectInstance.Status = *status

	if err := r.ssaStatus(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ErrorUpdatingStatus", fmt.Sprintf("updating state to %v", string(status.State)))
		return fmt.Errorf("error while updating status %s to: %w", status.State, err)
	}

	r.Event(objectInstance, "Normal", "StatusUpdated", fmt.Sprintf("updating state to %v", string(status.State)))
	return nil
}

func (r *SampleReconciler) processResources(ctx context.Context, objectInstance *v1alpha1.Sample) error {
	logger := log.FromContext(ctx)

	resourceObjs, err := getResourcesFromLocalPath(objectInstance.Spec.ResourceFilePath, logger)
	if err != nil {
		logger.Error(err, "error locating manifest of resources")
		return err
	}

	r.Event(objectInstance, "Normal", "ResourcesInstall", "installing resources")

	// the resources to be installed are unstructured,
	// so please make sure the types are available on the target cluster
	for _, obj := range resourceObjs.Items {
		if err = r.ssa(ctx, obj); err != nil && !errors2.IsAlreadyExists(err) {
			logger.Error(err, "error during installation of resources")
			return err
		}
	}
	return nil
}

func getStatusFromSample(objectInstance *v1alpha1.Sample) v1alpha1.SampleStatus {
	return objectInstance.Status
}

// getResourcesFromLocalPath returns resources from the dirPath in unstructured format.
// Only one file in .yaml or .yml format should be present in the target directory.
func getResourcesFromLocalPath(dirPath string, logger logr.Logger) (*ManifestResources, error) {
	dirEntries := make([]fs.DirEntry, 0)
	err := filepath.WalkDir(dirPath, func(path string, info fs.DirEntry, err error) error {
		// initial error
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		dirEntries, err = os.ReadDir(dirPath)
		return err
	})
	if err != nil {
		return nil, err
	}

	childCount := len(dirEntries)
	if childCount == 0 {
		logger.V(debugLogLevel).Info(fmt.Sprintf("no yaml file found at file path %s", dirPath))
		return nil, nil
	} else if childCount > 1 {
		logger.V(debugLogLevel).Info(fmt.Sprintf("more than one yaml file found at file path %s", dirPath))
		return nil, nil
	}
	file := dirEntries[0]
	allowedExtns := sets.NewString(".yaml", ".yml")
	if !allowedExtns.Has(filepath.Ext(file.Name())) {
		return nil, nil
	}

	fileBytes, err := os.ReadFile(filepath.Join(dirPath, file.Name()))
	if err != nil {
		return nil, fmt.Errorf("yaml file could not be read %s in dir %s: %w", file.Name(), dirPath, err)
	}
	return parseManifestStringToObjects(string(fileBytes))
}

// ssaStatus patches status using SSA on the passed object.
func (r *SampleReconciler) ssaStatus(ctx context.Context, obj client.Object) error {
	obj.SetManagedFields(nil)
	obj.SetResourceVersion("")
	return r.Status().Patch(ctx, obj, client.Apply,
		&client.SubResourcePatchOptions{PatchOptions: client.PatchOptions{FieldManager: fieldOwner}})
}

// ssa patches the object using SSA.
func (r *SampleReconciler) ssa(ctx context.Context, obj client.Object) error {
	obj.SetManagedFields(nil)
	obj.SetResourceVersion("")
	return r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))
}
