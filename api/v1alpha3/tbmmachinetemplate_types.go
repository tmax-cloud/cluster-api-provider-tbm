/*


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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TbmMachineTemplateSpec defines the desired state of TbmMachineTemplate
type TbmMachineTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Template TbmMachineTemplateResource `json:"template"`
}

type TbmMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec TbmMachineSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tbmmachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// TbmMachineTemplate is the Schema for the tbmmachinetemplates API
type TbmMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TbmMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TbmMachineTemplateList contains a list of TbmMachineTemplate
type TbmMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TbmMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TbmMachineTemplate{}, &TbmMachineTemplateList{})
}
