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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	"golang.org/x/time/rate"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	yamlUtil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyma-project/module-manager/pkg/types"
	"github.com/kyma-project/template-operator/api/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
)

const (
	requeueInterval = time.Second * 3
	finalizer       = "sample-finalizer"
	debugLogLevel   = 2
	fieldOwner      = "sample-field-owner"
)

var (
	ConditionTypeInstallation = "Installation"
	ConditionReasonReady      = "Ready"
)

// SampleReconciler reconciles a Sample object
type SampleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	*rest.Config
	// EventRecorder for creating k8s events
	record.EventRecorder
}

type RateLimiter struct {
	Burst           int
	Frequency       int
	BaseDelay       time.Duration
	FailureMaxDelay time.Duration
}

//+kubebuilder:rbac:groups=operator.kyma-project.io,resources=samples,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.kyma-project.io,resources=samples/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.kyma-project.io,resources=samples/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete

// TODO: dynamically create RBACs! Remove line below.
//+kubebuilder:rbac:groups="*",resources="*",verbs="*"

// SetupWithManager sets up the controller with the Manager.
func (r *SampleReconciler) SetupWithManager(mgr ctrl.Manager, rateLimiter RateLimiter, chartPath string) error {
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

// TemplateRateLimiter implements a rate limiter for a client-go.workqueue.  It has
// both an overall (token bucket) and per-item (exponential) rate limiting.
func TemplateRateLimiter(failureBaseDelay time.Duration, failureMaxDelay time.Duration,
	frequency int, burst int,
) ratelimiter.RateLimiter {
	return workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(failureBaseDelay, failureMaxDelay),
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(frequency), burst)})
}

// Reconcile is the entry point from the controller-runtime framework.
// It performs a reconciliation based on the passed ctrl.Request object.
func (r *SampleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	sampleObj := v1alpha1.Sample{}

	// verify resource interface compliance
	objectInstance, ok := sampleObj.DeepCopyObject().(types.CustomObject)
	if !ok {
		return ctrl.Result{}, getTypeError(req.String())
	}

	if err := r.Client.Get(ctx, req.NamespacedName, objectInstance); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		logger.Info(req.NamespacedName.String() + " got deleted!")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// check if deletionTimestamp is set, retry until it gets deleted
	status := getStatusFromObjectInstance(objectInstance)

	// set state to Deleting if not set for an object with deletion timestamp
	if !objectInstance.GetDeletionTimestamp().IsZero() &&
		status.State != types.StateDeleting {
		// if the status is not yet set to deleting, also update the status
		return ctrl.Result{}, r.setStatusForObjectInstance(ctx, objectInstance, status.WithState(types.StateDeleting))
	}

	// add finalizer if not present
	if controllerutil.AddFinalizer(objectInstance, finalizer) {
		objectInstance.SetManagedFields(nil)
		return ctrl.Result{}, r.ssa(ctx, objectInstance)
	}

	switch status.State {
	case "":
		return ctrl.Result{}, r.HandleInitialState(ctx, objectInstance)
	case types.StateProcessing:
		return ctrl.Result{Requeue: true}, r.HandleProcessingState(ctx, objectInstance)
	case types.StateDeleting:
		return ctrl.Result{Requeue: true}, r.HandleDeletingState(ctx, objectInstance)
	case types.StateError:
		return ctrl.Result{Requeue: true}, r.HandleProcessingState(ctx, objectInstance)
	case types.StateReady:
		return ctrl.Result{RequeueAfter: requeueInterval}, r.HandleReadyState(ctx, objectInstance)
	}

	return ctrl.Result{}, nil
}

// HandleInitialState bootstraps state handling for the reconciled resource.
func (r *SampleReconciler) HandleInitialState(ctx context.Context, objectInstance types.CustomObject) error {
	status := getStatusFromObjectInstance(objectInstance)

	installationCondition := metav1.Condition{
		Type:               ConditionTypeInstallation,
		Reason:             ConditionReasonReady,
		Status:             metav1.ConditionFalse,
		Message:            "installation is ready and resources can be used",
		ObservedGeneration: objectInstance.GetGeneration(),
	}

	status.Conditions = make([]metav1.Condition, 0, 1)
	meta.SetStatusCondition(&status.Conditions, installationCondition)

	return r.setStatusForObjectInstance(ctx, objectInstance, status.WithState(types.StateProcessing))
}

// HandleProcessingState processes the reconciled resource by processing the underlying resources.
// Based on the processing either a success or failure state is set on the reconciled resource.
func (r *SampleReconciler) HandleProcessingState(ctx context.Context, objectInstance types.CustomObject) error {
	r.Event(objectInstance, "Warning", "Processing", "resource processing")
	logger := log.FromContext(ctx)
	sampleObject, ok := objectInstance.(*v1alpha1.Sample)
	if !ok {
		return fmt.Errorf("type interface compliance of passed resource failed for %s",
			client.ObjectKeyFromObject(objectInstance).String())
	}

	status := getStatusFromObjectInstance(objectInstance)

	resourceObjs, err := getResourcesFromLocalPath(sampleObject.Spec.ResourceFilePath, logger)
	if err != nil {
		return err
	}

	// the resources to be installed are unstructured,
	// so please make sure the types are available on the target cluster
	for _, obj := range resourceObjs.Items {
		r.Event(objectInstance, "Normal", "ResourcesInstall", "installing resources")
		if err = r.ssa(ctx, obj); err != nil && !errors2.IsAlreadyExists(err) {
			logger.Error(err, "error during installation of resources")
			r.Event(objectInstance, "Warning", "ResourcesInstall", "installing resources error")
			return r.setStatusForObjectInstance(ctx, objectInstance, status.WithState(types.StateError))
		}
	}

	status.Conditions[0].Status = metav1.ConditionTrue
	status.Conditions[0].ObservedGeneration = objectInstance.GetGeneration()
	meta.SetStatusCondition(&status.Conditions, status.Conditions[0])
	return r.setStatusForObjectInstance(ctx, objectInstance, status.WithState(types.StateReady))
}

// HandleDeletingState processed the deletion on the reconciled resource.
// Once the deletion if processed the relevant finalizers (if applied) are removed.
func (r *SampleReconciler) HandleDeletingState(ctx context.Context, objectInstance types.CustomObject) error {
	r.Event(objectInstance, "Warning", "Deleting", "resource deleting")
	logger := log.FromContext(ctx)
	sampleObject, ok := objectInstance.(*v1alpha1.Sample)
	if !ok {
		return fmt.Errorf("type conversion of passed resource failed for %s",
			client.ObjectKeyFromObject(objectInstance).String())
	}

	status := getStatusFromObjectInstance(objectInstance)

	resourceObjs, err := getResourcesFromLocalPath(sampleObject.Spec.ResourceFilePath, logger)
	r.Event(objectInstance, "Normal", "ResourcesDelete", "deleting resources")

	// the resources to be installed are unstructured,
	// so please make sure the types are available on the target cluster
	for _, obj := range resourceObjs.Items {
		if err = r.Client.Delete(ctx, obj); err != nil && !errors2.IsNotFound(err) {
			logger.Error(err, "error during installation of resources")
			r.Event(objectInstance, "Warning", "ResourcesDelete", "deleting resources error")
			return r.setStatusForObjectInstance(ctx, objectInstance, status.WithState(types.StateError))
		}
	}

	// if resources are ready to be deleted, remove finalizer
	if controllerutil.RemoveFinalizer(objectInstance, finalizer) {
		return r.Client.Update(ctx, objectInstance)
	}
	return nil
}

// HandleReadyState checks for the consistency of reconciled resource, by verifying the underlying resources.
func (r *SampleReconciler) HandleReadyState(ctx context.Context, objectInstance types.CustomObject) error {
	// TODO: handle custom ready state handling here
	// by default we will call HandleProcessingState to verify if all resources were installed correctly
	// by patching them again
	return r.HandleProcessingState(ctx, objectInstance)
}

func (r *SampleReconciler) setStatusForObjectInstance(ctx context.Context, objectInstance types.CustomObject,
	status types.Status,
) error {
	objectInstance.SetStatus(status)

	if err := r.ssaStatus(ctx, objectInstance); err != nil {
		r.Event(objectInstance, "Warning", "ErrorUpdatingStatus", fmt.Sprintf("updating state to %v", string(status.State)))
		return fmt.Errorf("error while updating status %s to: %w", status.State, err)
	}

	r.Event(objectInstance, "Normal", "StatusUpdated", fmt.Sprintf("updating state to %v", string(status.State)))
	return nil
}

func getTypeError(namespacedName string) error {
	return fmt.Errorf("invalid custom resource object type for reconciliation %s", namespacedName)
}

func getStatusFromObjectInstance(objectInstance types.CustomObject) types.Status {
	return objectInstance.GetStatus()
}

// getResourcesFromLocalPath returns resources from the dirPath in unstructured format.
// Only one file in .yaml or .yml format should be present in the target directory.
func getResourcesFromLocalPath(dirPath string, logger logr.Logger) (*types.ManifestResources, error) {
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

// parseManifestStringToObjects parses the string of resources into a list of unstructured resources.
func parseManifestStringToObjects(manifest string) (*types.ManifestResources, error) {
	objects := &types.ManifestResources{}
	reader := yamlUtil.NewYAMLReader(bufio.NewReader(strings.NewReader(manifest)))
	for {
		rawBytes, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return objects, nil
			}

			return nil, fmt.Errorf("invalid YAML doc: %w", err)
		}

		rawBytes = bytes.TrimSpace(rawBytes)
		unstructuredObj := unstructured.Unstructured{}
		if err := yaml.Unmarshal(rawBytes, &unstructuredObj); err != nil {
			objects.Blobs = append(objects.Blobs, append(bytes.TrimPrefix(rawBytes, []byte("---\n")), '\n'))
		}

		if len(rawBytes) == 0 || bytes.Equal(rawBytes, []byte("null")) || len(unstructuredObj.Object) == 0 {
			continue
		}

		objects.Items = append(objects.Items, &unstructuredObj)
	}
}

// ssaStatus patches status using SSA on the passed object
func (r *SampleReconciler) ssaStatus(ctx context.Context, obj client.Object) error {
	obj.SetManagedFields(nil)
	return r.Status().Patch(ctx, obj, client.Apply, client.FieldOwner(fieldOwner))
}

// ssaStatus patches the object using SSA
func (r *SampleReconciler) ssa(ctx context.Context, obj client.Object) error {
	return r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))
}
