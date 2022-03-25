/*
Copyright 2022 kuizhiqing.

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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KIND = "TJob"

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type JobPhase string

const (
	Starting    JobPhase = "Starting"
	Pending     JobPhase = "Pending"
	Scaling     JobPhase = "Scaling"
	Aborting    JobPhase = "Aborting"
	Aborted     JobPhase = "Aborted"
	Running     JobPhase = "Running"
	Restarting  JobPhase = "Restarting"
	Completing  JobPhase = "Completing"
	Completed   JobPhase = "Completed"
	Terminating JobPhase = "Terminating"
	Terminated  JobPhase = "Terminated"
	Failed      JobPhase = "Failed"
	Succeed     JobPhase = "Succeed"
	Unknown     JobPhase = "Unknown"
)

type Intranet string

const (
	Default     Intranet = "Default"
	PodIP       Intranet = "PodIP"
	Service     Intranet = "Service"
	HostNetwork Intranet = "HostNetwork"
)

type CleanPolicy string

const (
	// CleanAlways policy will always clean pods
	CleanAlways CleanPolicy = "Always"
	// CleanNever policy will nerver clean pods
	CleanNever CleanPolicy = "Never"
	// CleanOnFailure policy will clean pods only on job failed
	CleanOnFailure CleanPolicy = "OnFailure"
	// CleanOnCompletion policy will clean pods only on job completed
	CleanOnCompletion CleanPolicy = "OnCompletion"
)

type StartPolicy string

const (
	StartAlways    StartPolicy = "Always"
	StartRunning   StartPolicy = "Running"
	StartSucceeded StartPolicy = "Succeeded"
)

type TaskSpec struct {
	// Replicas replica
	Replicas int `json:"replicas"`

	// Requests set the minimal replicas of server to be run
	Requests *int `json:"requests,omitempty"`

	// Requests set the maximal replicas of server to be run
	Limits *int `json:"limits,omitempty"`

	// Template specifies the podspec of a server
	Template corev1.PodTemplateSpec `json:"template,omitempty"`

	// Name name the resource, validated by
	Name string `json:"name"`
}

type TaskStatus struct {
	// Pending
	Pending int `json:"pending,omitempty"`
	// Starting
	Starting int `json:"starting,omitempty"`
	// Running
	Running int `json:"running,omitempty"`
	// Failed
	Failed int `json:"failed,omitempty"`
	// Success
	Succeeded int `json:"succeeded,omitempty"`
	// Unknown
	Unknown int `json:"unknown,omitempty"`
	// A list of pointer to pods
	Refs []corev1.ObjectReference `json:"refs,omitempty"`
}

// TJobSpec defines the desired state of TJob
type TJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// CleanPodPolicy defines whether to clean pod after job finished, default Never
	// +optional
	CleanPodPolicy CleanPolicy `json:"cleanPodPolicy,omitempty"`

	// Intranet defines the communication mode inter pods : PodIP, Service or Host
	// +optional
	Intranet Intranet `json:"intranet,omitempty"`

	// Elastic indicates the elastic level
	// +optional
	Elastic *int32 `json:"elastic,omitempty"`

	// Tasks defines the resources in list, the order determinate the running order
	Tasks []*TaskSpec `json:"tasks,omitempty"`

	// Framework defines the framework of the job
	Framework *string `json:"framework,omitempty"`

	// StartPolicy indicates how the tasks in the list start depend on previous status
	// +optional
	StartPolicy *StartPolicy `json:"startPolicy,omitempty"`
}

// TJobStatus defines the observed state of TJob
type TJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// The latest available observations of an object's current state.
	Conditions []batchv1.JobCondition `json:"conditions,omitempty"`

	// The phase of TJob.
	Phase JobPhase `json:"phase,omitempty"`

	// ResourceStatues of tasks
	Tasks map[string]*TaskStatus `json:"tasks,omitempty"`

	// StartTime indicate when the job started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime indicate when the job completed/failed
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	ObservedGeneration int `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TJob is the Schema for the tjobs API
type TJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TJobSpec   `json:"spec,omitempty"`
	Status TJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TJobList contains a list of TJob
type TJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TJob{}, &TJobList{})
}
