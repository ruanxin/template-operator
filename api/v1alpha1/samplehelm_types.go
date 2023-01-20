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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SampleHelmSpec defines the desired state of SampleHelm
type SampleHelmSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ChartPath represents the local path to the Helm chart
	ChartPath string `json:"chartPath,omitempty"`
}

// SampleHelmStatus defines the observed state of SampleHelm
type SampleHelmStatus struct {
	Status `json:",inline"`

	// Conditions contain a set of conditionals to determine the State of Status.
	// If all Conditions are met, State is expected to be in StateReady.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// add other fields to status subresource here
}

func (s *SampleHelmStatus) WithState(state State) *SampleHelmStatus {
	s.State = state
	return s
}

func (s *SampleHelmStatus) WithInstallConditionStatus(status metav1.ConditionStatus, objGeneration int64) *SampleHelmStatus {
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

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SampleHelm is the Schema for the samplehelms API
type SampleHelm struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SampleHelmSpec   `json:"spec,omitempty"`
	Status SampleHelmStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SampleHelmList contains a list of SampleHelm
type SampleHelmList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SampleHelm `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SampleHelm{}, &SampleHelmList{})
}
