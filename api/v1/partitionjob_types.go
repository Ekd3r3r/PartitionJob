/*
Copyright 2023.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PartitionJobSpec defines the desired state of PartitionJob
type PartitionJobSpec struct {

	// Replicas define the number of pod replicas to be created
	Replicas *int64 `json:"replicas,omitempty"`

	// Partitions define the number of pods being upgraded to the new version
	Partitions *int64 `json:"partitions,omitempty"`

	
    // Specifies the strategy used to upgrade the number of replicas in a Partition Job
    // Valid values are:
    // - "All" (default): allows all replicas to be upgraded to the new version;
    // - "PartitionOnly": allows only the number of replicas specified in the partition to be upgraded to the new version;

    // +optional
    UpgradeStrategy UpgradeStrategy `json:"upgradeStrategy,omitempty"`
}

// UpgradeStrategy describes how the job will be handled.
// Only one of the following upgrade strategies may be specified.
// If none of the following startegies is specified, the default one
// is UpgradeAll.
// +kubebuilder:validation:Enum=All;PartitionOnly
type UpgradeStrategy string

const (
    // UpgradeAll allows all replicas to be upgraded to the new version.
    UpgradeAll UpgradeStartegy = "All"

	// UpgradePartitionOnly allows only the number of replicas specified in the partition to be upgraded to the new version.
    UpgradePartitionOnly ConcurrencyPolicy = "PartitionOnly"

)

// PartitionJobStatus defines the observed state of PartitionJob
type PartitionJobStatus struct {

	
	// ActiveReplicas define the number of pod replicas currently active
	ActiveReplicas *int64 `json:"activeReplicas,omitempty"`

	// ActivePartition define the number of pod replicas currently upgraded to the new version
	ActivePartitions *int64 `json:"activePartitions,omitempty"`


}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PartitionJob is the Schema for the partitionjobs API
type PartitionJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PartitionJobSpec   `json:"spec,omitempty"`
	Status PartitionJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PartitionJobList contains a list of PartitionJob
type PartitionJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PartitionJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PartitionJob{}, &PartitionJobList{})
}
