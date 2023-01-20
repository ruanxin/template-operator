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
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlUtil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"helm.sh/helm/v3/pkg/chart/loader"

	"github.com/kyma-project/template-operator/api/v1alpha1"
)

// SampleHelmReconciler reconciles a SampleHelm object
type SampleHelmReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// EventRecorder for creating k8s events
	record.EventRecorder

	Config *rest.Config
}

type ManifestResources struct {
	Items []*unstructured.Unstructured
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

// SetupWithManager sets up the controller with the Manager.
func (r *SampleHelmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.SampleHelm{}).
		Complete(r)
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

// ssaStatus patches the object using SSA
func (r *SampleHelmReconciler) ssa(ctx context.Context, obj client.Object) error {
	obj.SetManagedFields(nil)
	obj.SetResourceVersion("")
	return r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))
}

func (r *SampleHelmReconciler) Render(ctx context.Context, obj *v1alpha1.SampleHelm) (*ManifestResources, error) {
	//status := obj.Status
	logger := log.FromContext(ctx)

	actionConfig := new(action.Configuration)
	settings := cli.New()
	if err := actionConfig.Init(NewRESTClientGetter(r.Config), settings.Namespace(), "secrets", func(format string, v ...interface{}) {
		format = fmt.Sprintf("[debug] %s\n", format)
		debugLevel := 2
		logger.V(debugLevel).Info(fmt.Sprintf(format, v...))
	}); err != nil {
		logger.Error(err, "")
	}

	actionClient := action.NewInstall(actionConfig)

	// Helm flags
	r.SetDefaultClientConfig(actionClient, "sample-release-name")

	// Helm values
	valuesAsMap := map[string]interface{}{
		"label": "custom-label-from-controller",
	}

	chrt, err := loader.Load(obj.Spec.ChartPath)
	if err != nil {
		return nil, err
	}

	release, err := actionClient.RunWithContext(ctx, chrt, valuesAsMap)
	if err != nil {
		return nil, err
	}

	return parseManifestStringToObjects(release.Manifest)
}

func (r *SampleHelmReconciler) SetDefaultClientConfig(actionClient *action.Install, releaseName string) {
	actionClient.DryRun = true
	actionClient.Atomic = false
	actionClient.Wait = false
	actionClient.WaitForJobs = false
	actionClient.Replace = true     // Skip the name check
	actionClient.IncludeCRDs = true // include CRDs in the templated output
	actionClient.ClientOnly = true
	actionClient.ReleaseName = releaseName
	actionClient.Namespace = v1.NamespaceDefault

	// default versioning if unspecified
	if actionClient.Version == "" && actionClient.Devel {
		actionClient.Version = ">0.0.0-0"
	}
}

func parseManifestStringToObjects(manifest string) (*ManifestResources, error) {
	objects := &ManifestResources{}
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
			return nil, err
		}

		if len(rawBytes) == 0 || bytes.Equal(rawBytes, []byte("null")) || len(unstructuredObj.Object) == 0 {
			continue
		}

		objects.Items = append(objects.Items, &unstructuredObj)
	}
}

func (r *SampleHelmReconciler) HandleInitialState(ctx context.Context, v *v1alpha1.SampleHelm) error {

}

func (r *SampleHelmReconciler) HandleProcessingState(ctx context.Context, obj *v1alpha1.SampleHelm) error {
	resources, err := r.Render(ctx, obj)
	if err != nil {
		return err
	}

	installErrs := make([]error, 0)

	// instead of looping a concurrent mechanism can also be implemented
	for _, resource := range resources.Items {
		err = r.ssa(ctx, resource)
		if err != nil {
			installErrs = append(installErrs, err)
		}
	}

	if len(installErrs) != 0 {
		buf := &bytes.Buffer{}
		for _, err := range installErrs {
			_, _ = fmt.Fprintf(buf, "%v\n", err.Error())
		}
		return fmt.Errorf(buf.String())
	}
}

func (r *SampleHelmReconciler) HandleDeletingState(ctx context.Context, v *v1alpha1.SampleHelm) error {

}

func (r *SampleHelmReconciler) HandleErrorState(ctx context.Context, v *v1alpha1.SampleHelm) error {

}

func (r *SampleHelmReconciler) HandleReadyState(ctx context.Context, v *v1alpha1.SampleHelm) error {

}
