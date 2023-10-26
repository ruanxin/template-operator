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

// Package v1alpha1 contains API Schema definitions for the component v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=operator.kyma-project.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	SampleKind Kind = "Sample"
	Version    Kind = "v1alpha1"
)

type Kind string

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "operator.kyma-project.io", Version: "v1alpha1"}

	ConditionTypeInstallation = "Installation"
	ConditionReasonReady      = "Ready"
)

type SampleStatus struct {
	Status `json:",inline"`

	// Conditions contain a set of conditionals to determine the State of Status.
	// If all Conditions are met, State is expected to be in StateReady.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (s *SampleStatus) WithState(state State) *SampleStatus {
	s.State = state
	return s
}

func (s *SampleStatus) WithInstallConditionStatus(status metav1.ConditionStatus, objGeneration int64) *SampleStatus {
	if s.Conditions == nil {
		s.Conditions = make([]metav1.Condition, 0, 1)
	}

	condition := meta.FindStatusCondition(s.Conditions, ConditionTypeInstallation)

	if condition == nil {
		condition = &metav1.Condition{
			Type:    ConditionTypeInstallation,
			Reason:  ConditionReasonReady,
			Message: "installation is ready and resources can be used",
		}
	}

	condition.Status = status
	condition.ObservedGeneration = objGeneration
	meta.SetStatusCondition(&s.Conditions, *condition)
	return s
}

type SampleSpec struct {
	// ResourceFilePath indicates the local dir path containing a .yaml or .yml,
	// with all required resources to be processed
	ResourceFilePath string `json:"resourceFilePath,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.state"

// Sample is the Schema for the samples API.
type Sample struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SampleSpec   `json:"spec,omitempty"`
	Status SampleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SampleList contains a list of Sample.
type SampleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sample `json:"items"`
}
