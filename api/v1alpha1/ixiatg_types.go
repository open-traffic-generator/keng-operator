/*
Copyright 2021.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type IxiaTGSvcPort struct {
	In  int32 `json:"in"`
	Out int32 `json:"out,omitempty"`
	//InIp     string `json:"inside_ip,omitempty"`
	//OutIp    string `json:"outside_ip,omitempty"`
	//NodePort int32 `json:"node_port,omitempty"`
}

//type IxiaTGEP struct {
//	Name string `json:"name"`
//	SvcPortMap map[string]IxiaTGSvcPort `json:"service_port_map,omitempty"`
//}

type IxiaTGIntf struct {
	Name    string `json:"name"`
	PodName string `json:"pod_name"`
	Group   string `json:"group,omitempty"`
}

type IxiaTGIntfMeta struct {
	Name    string `json:"name"`
	PodName string `json:"pod_name"`
}

// IxiaTGSpec defines the desired state of IxiaTG
type IxiaTGSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Version of the node
	Version string `json:"release,omitempty"`
	// Config of the node
	//Config string `json:"config,omitempty"`
	//ApiEndPoint IxiaTGEP `json:"api_endpoint"`
	ApiEndPoint map[string]IxiaTGSvcPort `json:"api_endpoint,omitempty"`
	Interfaces  []IxiaTGIntf             `json:"interfaces,omitempty"`
}

// IxiaTGStatus defines the observed state of IxiaTG
type IxiaTGStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	//Pod    string `json:"pod,omitempty"`
	Status         string           `json:"status,omitempty"`
	Reason         string           `json:"reason,omitempty"`
	InterfaceMetas []IxiaTGIntfMeta `json:"interface_metas,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// IxiaTG is the Schema for the ixiacs API
//+kubebuilder:subresource:status
type IxiaTG struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IxiaTGSpec   `json:"spec,omitempty"`
	Status IxiaTGStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IxiaTGList contains a list of IxiaTG
type IxiaTGList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IxiaTG `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IxiaTG{}, &IxiaTGList{})
}
