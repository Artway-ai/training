// Copyright 2021 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	tv1 "github.com/artway-ai/training/api/v1"
)

const (
	servicePortsNum           = 2
	initContainerName         = "init"
	initContainerCpu          = "10m"
	initContainerMem          = "10m"
	podNameKey                = "pod-name-key"
	initContainerImageKey     = "INIT_IMAGE"
	defaultInitContainerImage = "docker.io/library/busybox:1"
)

var (
	initContainerCmd     = []string{"sh", "-c", "while true; do if [ -f goon ]; then exit 0; else sleep 0.1; fi; done"}
	initContainerRelease = []string{"touch", "goon"}

	initContainerImage = func() string {
		e, ok := os.LookupEnv(initContainerImageKey)
		if ok {
			return e
		} else {
			return defaultInitContainerImage
		}
	}()
)

// status related

// The two following functions should be consistent
func getTaskName(tjobName, podName string) string {
	s := strings.TrimPrefix(podName, tjobName)
	return s[1 : len(s)-6]
}
func generatePodName(tjobName, taskName string) string {
	return fmt.Sprintf("%s-%s-%s", tjobName, taskName, utilrand.String(5))
}

func getTaskSpecByName(tj *tv1.TJob, taskName string) (res *tv1.TaskSpec) {
	for i, r := range tj.Spec.Tasks {
		if r.Name == taskName {
			return tj.Spec.Tasks[i]
		}
	}
	return nil
}

func isAllPodsReady(tj *tv1.TJob, ctrlPods *corev1.PodList) bool {
	if !isAllPodsCreated(tj) {
		return false
	}
	for _, pod := range ctrlPods.Items {
		if pod.Status.PodIP == "" {
			return false
		}
	}
	return true
}

func isAllPodsCreated(tj *tv1.TJob) bool {
	for _, t := range tj.Spec.Tasks {
		if !isPodCreated(t, tj.Status.Tasks[t.Name]) {
			return false
		}
	}
	return true
}

func isPodCreated(spec *tv1.TaskSpec, status *tv1.TaskStatus) bool {
	if spec == nil {
		return true
	}
	if status != nil && len(status.Refs) == spec.Replicas {
		return true
	}
	return false
}

func isFailed(status *tv1.TaskStatus) bool {
	return status != nil && status.Failed > 0
}
func isPending(status *tv1.TaskStatus) bool {
	return status != nil && status.Pending > 0
}
func isStarting(status *tv1.TaskStatus) bool {
	return status != nil && status.Starting > 0
}
func isRunning(spec *tv1.TaskSpec, status *tv1.TaskStatus) bool {
	return spec == nil || (status != nil && spec.Replicas == status.Running)
}
func isCompleted(spec *tv1.TaskSpec, status *tv1.TaskStatus) bool {
	return spec == nil || (status != nil && spec.Replicas == status.Succeeded)
}

func getTJobPhase(tj *tv1.TJob) tv1.JobPhase {

	// final phase won't change any more
	if tj.Status.Phase == tv1.Completed {
		return tv1.Completed
	} else if tj.Status.Phase == tv1.Failed {
		return tv1.Failed
	}

	for _, status := range tj.Status.Tasks {
		if isFailed(status) {
			return tv1.Failed
		} else if isStarting(status) {
			return tv1.Starting
		} else if isPending(status) {
			return tv1.Pending
		}
	}
	checkAll := func(check func(spec *tv1.TaskSpec, status *tv1.TaskStatus) bool) bool {
		for _, t := range tj.Spec.Tasks {
			if !check(t, tj.Status.Tasks[t.Name]) {
				return false
			}
		}
		return true
	}
	if checkAll(isRunning) {
		return tv1.Running
	}
	if checkAll(isCompleted) {
		return tv1.Completed
	}

	if tj.Status.Phase == "" {
		return tv1.Pending
	}

	return tj.Status.Phase
}

func isPodRealRuning(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		if !container.Ready {
			return false
		}
	}
	for i := range pod.Status.ContainerStatuses {
		container := pod.Status.ContainerStatuses[i]
		if !container.Ready || container.State.Running == nil {
			return false
		}
	}
	return true
}

func isAllInitContainerRunning(ctrlPods corev1.PodList) bool {
	for i, _ := range ctrlPods.Items {
		if !isInitContainerRunning(&ctrlPods.Items[i]) {
			return false
		}
	}
	return true
}

func isInitContainerRunning(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodPending {
		return false
	}
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		if container.Name == initContainerName && container.State.Running != nil {
			return true
		}
	}
	return false
}

func getTJobStartTime(tj *tv1.TJob) *metav1.Time {
	if tj.Status.StartTime.IsZero() && tj.Status.Phase == tv1.Running {
		tmp := metav1.Now()
		return &tmp
	}
	return tj.Status.StartTime
}

func getTJobCompleteTime(tj *tv1.TJob) *metav1.Time {
	if tj.Status.CompletionTime.IsZero() && (tj.Status.Phase == tv1.Completed || tj.Status.Phase == tv1.Failed) {
		tmp := metav1.Now()
		return &tmp
	}
	return tj.Status.CompletionTime
}

func buildService(pod corev1.Pod) *corev1.Service {
	var ports = []corev1.ServicePort{}
	for i := 0; i < servicePortsNum; i++ {
		ports = append(ports, corev1.ServicePort{
			Name: fmt.Sprintf("p-%d", i),
			Port: int32(8000 + i),
		})
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
			Selector: map[string]string{
				podNameKey: pod.Name,
			},
			ClusterIP: "None",
		},
	}
	return svc
}

func needCleanPods(tj *tv1.TJob) bool {
	if tj.Status.Phase == tv1.Failed {
		if tj.Spec.CleanPodPolicy == tv1.CleanAlways || tj.Spec.CleanPodPolicy == tv1.CleanOnFailure {
			return true
		}
	} else if tj.Status.Phase == tv1.Completed {
		if tj.Spec.CleanPodPolicy == "" || tj.Spec.CleanPodPolicy == tv1.CleanAlways || tj.Spec.CleanPodPolicy == tv1.CleanOnCompletion {
			return true
		}
	}
	return false
}

func getTaskSpecMap(tj *tv1.TJob) map[string]*tv1.TaskSpec {
	tsm := map[string]*tv1.TaskSpec{}
	for i, spec := range tj.Spec.Tasks {
		tsm[spec.Name] = tj.Spec.Tasks[i]
	}
	return tsm
}

func buildPodTemplate(tj *tv1.TJob, ts *tv1.TaskSpec) (pod *corev1.Pod) {
	name := generatePodName(tj.Name, ts.Name)

	pod = &corev1.Pod{}

	pod.ObjectMeta = *(ts.Template.ObjectMeta.DeepCopy())
	pod.Spec = *(ts.Template.Spec.DeepCopy())

	if pod.ObjectMeta.Labels == nil {
		pod.ObjectMeta.Labels = map[string]string{}
	}
	pod.ObjectMeta.Labels[podNameKey] = name

	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = map[string]string{}
	}
	pod.ObjectMeta.Annotations["task-name"] = ts.Name

	pod.ObjectMeta.Name = name
	pod.ObjectMeta.Namespace = tj.Namespace

	pod.Spec.Hostname = name
	pod.Spec.Subdomain = name

	envcm := corev1.EnvFromSource{
		ConfigMapRef: &corev1.ConfigMapEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: tj.Name,
			},
		},
	}

	coInit := corev1.Container{
		Name:            initContainerName,
		Image:           initContainerImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         initContainerCmd,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(initContainerCpu),
				corev1.ResourceMemory: resource.MustParse(initContainerMem),
				//corev1.ResourceEphemeralStorage: resource.MustParse(),
			},
		},
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, coInit)

	pod.Spec.Containers[0].EnvFrom = append(pod.Spec.Containers[0].EnvFrom, envcm)

	if tj.Spec.Intranet == tv1.HostNetwork {
		pod.Spec.HostNetwork = true
	}

	return pod
}

func buildConfigMap(tj *tv1.TJob, ctrlPods *corev1.PodList) (cm *corev1.ConfigMap) {
	envs := map[string][]string{}
	hosts := []string{}

	for _, pod := range ctrlPods.Items {
		if len(strings.Split(pod.Status.PodIP, ".")) != 4 {
			return nil
		}
		tn := getTaskName(tj.Name, pod.Name)
		envs[tn] = append(envs[tn], fmt.Sprintf("%s:%d", pod.Status.PodIP, 8888))
		hosts = append(hosts, pod.Status.PodIP)
	}

	cm = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{},
			Annotations: map[string]string{},
			Name:        tj.Name,
			Namespace:   tj.Namespace,
		},
		Data: map[string]string{
			"TJOB_NAME": tj.Name,
		},
	}

	for _, task := range tj.Spec.Tasks {
		tn := strings.ReplaceAll(task.Name, "-", "_")
		envKey := fmt.Sprintf("%s_ENDPOINTS", strings.ToUpper(tn))
		cm.Data[envKey] = strings.Join(envs[task.Name], ",")
	}
	cm.Data["TJOB_ENDPOINTS"] = strings.Join(hosts, ",")

	// TODO(kuizhiqing) handle the following section

	if tj.Spec.Framework != nil && *tj.Spec.Framework == "paddle" {
		cm.Data["PADDLE_JOB_ID"] = tj.Name
		cm.Data["PADDLE_MASTER"] = fmt.Sprintf("%s:8090", hosts[0])
		cm.Data["PADDLE_NNODES"] = fmt.Sprintf("%d", len(hosts))
	}

	if tj.Spec.Framework != nil && *tj.Spec.Framework == "torch" {
		cm.Data["PET_RDZV_ID"] = tj.Name
		cm.Data["PET_RDZV_BACKEND"] = "c10d"
		cm.Data["PET_RDZV_ENDPOINT"] = fmt.Sprintf("%s", hosts[0])
		cm.Data["PET_NPROC_PER_NODE"] = "auto"
		cm.Data["PET_NNODES"] = fmt.Sprintf("%d", len(hosts))
	}

	return cm
}
