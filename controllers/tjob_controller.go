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

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	slog "log"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	tv1 "github.com/artway-ai/training/api/v1"
	"github.com/artway-ai/training/pkg/ttlcache"
)

var (
	ctrlRefKey    = ".metadata.controller"
	apiGVStr      = tv1.GroupVersion.String()
	finalizerName = "finalizers.artway.ai"

	deleteCacheKey  = "deleteall"
	createCacheKey  = "createall"
	releaseCacheKey = "releasekey"
)

type ReconcilerConfig struct {
	// for extension
}

// TJobReconciler reconciles a TJob object
type TJobReconciler struct {
	client.Client
	Config   *ReconcilerConfig
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme

	restClient rest.Interface
	restConfig *rest.Config
	cache      *ttlcache.Cache
}

//+kubebuilder:rbac:groups=training.artway.ai,resources=tjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=training.artway.ai,resources=tjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=training.artway.ai,resources=tjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;create
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services/status,verbs=get
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps/status,verbs=get
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the TJob object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *TJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	tj := &tv1.TJob{}
	if err := r.Get(ctx, req.NamespacedName, tj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logr.Info("Reconcile", "version", tj.ResourceVersion, "phase", tj.Status.Phase, "delete", tj.ObjectMeta.DeletionTimestamp)

	if ret, err := r.finalize(ctx, tj); ret {
		return ctrl.Result{RequeueAfter: time.Second}, err
	}

	ctrlPods := &corev1.PodList{}
	if err := r.List(ctx, ctrlPods, client.InNamespace(req.Namespace), client.MatchingFields{ctrlRefKey: req.Name}); err != nil {
		return ctrl.Result{}, err
	}

	if ret, err := r.updateStatus(ctx, tj, ctrlPods); ret {
		return ctrl.Result{}, err
	}

	if ret, err := r.cleanPods(ctx, tj, ctrlPods); ret {
		return ctrl.Result{}, err
	}

	if ret, err := r.createPods(ctx, tj); ret {
		return ctrl.Result{RequeueAfter: time.Second * 2}, err
	}

	if ret, err := r.constructNetwork(ctx, tj, ctrlPods); ret {
		return ctrl.Result{RequeueAfter: time.Second * 1}, err
	}

	if ret, err := r.generateJobConfig(ctx, tj, ctrlPods); ret {
		return ctrl.Result{RequeueAfter: time.Second * 3}, err
	}

	if ret, err := r.runPods(ctx, tj, ctrlPods); ret {
		return ctrl.Result{RequeueAfter: time.Second * 1}, err
	}

	return ctrl.Result{}, nil
}

func (r *TJobReconciler) finalize(ctx context.Context, tj *tv1.TJob) (bool, error) {
	if tj.ObjectMeta.DeletionTimestamp.IsZero() {
		if !containsString(tj.ObjectMeta.Finalizers, finalizerName) {
			tj.ObjectMeta.Finalizers = append(tj.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(ctx, tj); err != nil {
				return true, err
			}
		}
	} else {
		if containsString(tj.ObjectMeta.Finalizers, finalizerName) {
			// do before delete
			tj.ObjectMeta.Finalizers = removeString(tj.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(ctx, tj); err != nil {
				return true, err
			}
		}
		return false, nil
	}
	return false, nil
}

func (r *TJobReconciler) updateStatus(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) (bool, error) {
	tj.Status = tv1.TJobStatus{
		Phase:          getTJobPhase(tj),
		StartTime:      getTJobStartTime(tj),
		CompletionTime: getTJobCompleteTime(tj),
		Tasks:          r.getTaskStatus(ctx, tj, ctrlPods),
	}
	if err := r.Status().Update(ctx, tj); err != nil {
		if apierrors.IsConflict(err) {
			return true, nil
		}
		return true, err
	}
	return false, nil
}

func (r *TJobReconciler) getTaskStatus(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) map[string]*tv1.TaskStatus {
	syncStatusByPod := func(ss *tv1.TaskStatus, pod *corev1.Pod) {
		switch pod.Status.Phase {
		case corev1.PodPending:
			if isInitContainerRunning(pod) {
				ss.Starting++
			} else {
				ss.Pending++
			}
		case corev1.PodRunning:
			if isPodRealRuning(pod) {
				ss.Running++
			} else {
				ss.Starting++
			}
		case corev1.PodFailed:
			ss.Failed++
		case corev1.PodSucceeded:
			ss.Succeeded++
		case corev1.PodUnknown:
			ss.Unknown++
		}
		pref, err := ref.GetReference(r.Scheme, pod)
		if err != nil {
			return
		}
		ss.Refs = append(ss.Refs, *pref)
	}

	ts := map[string]*tv1.TaskStatus{}
	for i, pod := range ctrlPods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		n := getTaskName(tj.Name, pod.Name)
		if ts[n] == nil {
			ts[n] = &tv1.TaskStatus{}
		}
		syncStatusByPod(ts[n], &ctrlPods.Items[i])
	}
	return ts
}

func (r *TJobReconciler) cleanPods(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) (bool, error) {
	// clean pod surplus
	ct := map[string]int{}
	for i, pod := range ctrlPods.Items {
		if pod.DeletionTimestamp != nil {
			return true, nil
		}
		n := getTaskName(tj.Name, pod.Name)
		ct[n] += 1
		if ct[n] > getTaskSpecByName(tj, n).Replicas {
			slog.Println("do delete")
			r.deleteResource(ctx, tj, &ctrlPods.Items[i])
		}
	}

	if needCleanPods(tj) && len(ctrlPods.Items) > 1 {
		if _, exists := r.cache.Get(deleteCacheKey); exists {
			slog.Println("delete pod cache")
			return true, nil
		}
		if err := r.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(tj.Namespace), client.MatchingFields{ctrlRefKey: tj.Name}); err != nil {
			slog.Println("do clean")
			r.cache.SetTTL(deleteCacheKey, time.Second*3)
			return true, nil
		}
	}
	return false, nil
}

func (r *TJobReconciler) createPods(ctx context.Context, tj *tv1.TJob) (bool, error) {
	if needCleanPods(tj) {
		return false, nil
	}

	for i, task := range tj.Spec.Tasks {
		diff := task.Replicas
		if tj.Status.Tasks[task.Name] != nil {
			diff = task.Replicas - len(tj.Status.Tasks[task.Name].Refs)
		}
		if diff > 0 {
			slog.Println("diff", diff)
			if _, exists := r.cache.Get(createCacheKey); exists {
				slog.Println("cache do not create")
				return true, nil
			}
			if ret, err := r.createPodsBatch(ctx, tj, tj.Spec.Tasks[i], diff); ret {
				r.cache.SetTTL(createCacheKey, time.Second*2)
				return true, err
			}
		}
	}
	return false, nil
}

func (r *TJobReconciler) createPodsBatch(ctx context.Context, tj *tv1.TJob, ts *tv1.TaskSpec, diff int) (bool, error) {
	running := make(chan bool, 10)
	completed := make(chan bool, diff)
	failed := make(chan error, diff)
	defer close(running)
	defer close(completed)
	defer close(failed)

	for i := 0; i < diff; i++ {
		running <- true
		go func() {
			pod := buildPodTemplate(tj, ts)
			if err := r.createResourceWithCtrl(ctx, tj, pod); err != nil {
				failed <- err
				return
			}
			<-running
			completed <- true
		}()
	}

	var err error
	ret := false
	for i := 0; i < diff; i++ {
		select {
		case err = <-failed:
			ret = true
		case <-completed:
		}
	}
	return ret, err
}

func (r *TJobReconciler) constructNetwork(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) (bool, error) {
	if tj.Spec.Intranet == tv1.Service {
		if err := r.createService(ctx, tj, ctrlPods); err != nil {
			return true, err
		}
	} else if tj.Spec.Intranet == tv1.HostNetwork {
		//TODO(k)
	}
	return false, nil
}

func (r *TJobReconciler) createService(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) error {
	var svcs corev1.ServiceList
	if err := r.List(ctx, &svcs, client.InNamespace(tj.Namespace), client.MatchingFields{ctrlRefKey: tj.Name}); err != nil {
		return err
	}
	if len(svcs.Items) == len(ctrlPods.Items) {
		return nil
	}
	slog.Println("do create svc")
	for _, pod := range ctrlPods.Items {
		svc := buildService(pod)
		if err := r.Get(ctx, client.ObjectKeyFromObject(svc), &corev1.Service{}); err == nil {
			continue
		} else if apierrors.IsNotFound(err) {
			if err = r.createResourceWithCtrl(ctx, tj, svc); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *TJobReconciler) generateJobConfig(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) (bool, error) {
	if isAllPodsReady(tj, ctrlPods) {
		if err := r.createConfigMap(ctx, tj, ctrlPods); err != nil {
			return true, err
		}
	} else {
		return true, nil
	}
	return false, nil
}

func (r *TJobReconciler) createConfigMap(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) error {
	if err := r.Get(ctx, types.NamespacedName{Name: tj.Name, Namespace: tj.Namespace}, &corev1.ConfigMap{}); err != nil && apierrors.IsNotFound(err) {
		cm := buildConfigMap(tj, ctrlPods)
		if cm == nil {
			return nil
		}
		slog.Println("do create cm")
		if err := r.createResourceWithCtrl(ctx, tj, cm); err != nil {
			return err
		}
	}
	return nil
}

func (r *TJobReconciler) runPods(ctx context.Context, tj *tv1.TJob, ctrlPods *corev1.PodList) (bool, error) {
	releaseTask := func(taskName string) {
		for i, pod := range ctrlPods.Items {
			if taskName == getTaskName(tj.Name, pod.Name) {
				if isInitContainerRunning(&ctrlPods.Items[i]) {
					r.execInPod(tj.Namespace, pod.Name, initContainerName, initContainerRelease)
				}
			}
		}
	}

	// initinate ensure pod run in the defined order
	if tj.Status.Phase == tv1.Starting {
		if _, exists := r.cache.Get(releaseCacheKey); exists {
			slog.Println("wait release key")
			return true, nil
		}
		for _, task := range tj.Spec.Tasks {
			if tj.Spec.StartPolicy == nil || *tj.Spec.StartPolicy == tv1.StartImmediately {
				slog.Println("do release", task.Name)
				releaseTask(task.Name)
			} else if *tj.Spec.StartPolicy == tv1.StartAfterRunning {
				if status := tj.Status.Tasks[task.Name]; status != nil && status.Running < task.Replicas {
					slog.Println("do release", task.Name)
					releaseTask(task.Name)
					break
				}
			} else if *tj.Spec.StartPolicy == tv1.StartAfterSucceeded {
				if status := tj.Status.Tasks[task.Name]; status != nil && status.Succeeded < task.Replicas {
					slog.Println("do release", task.Name)
					releaseTask(task.Name)
					break
				}
			}
		}
		r.cache.SetTTL(releaseCacheKey, time.Second*2)
		return false, nil
	}
	return false, nil
}

func (r *TJobReconciler) deleteResource(ctx context.Context, robj runtime.Object, obj client.Object) error {
	if obj.GetDeletionTimestamp() != nil {
		return nil
	}
	tp := obj.GetObjectKind().GroupVersionKind().Kind
	if err := r.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
		r.Recorder.Event(robj, corev1.EventTypeWarning, "Delete", fmt.Sprintf("delete failed %s %s", tp, obj.GetName()))
		return err
	}
	r.Recorder.Event(robj, corev1.EventTypeNormal, "Deleted", fmt.Sprintf("deleted %s %s", tp, obj.GetName()))
	return nil
}

func (r *TJobReconciler) createResourceWithCtrl(ctx context.Context, tj *tv1.TJob, obj client.Object) error {
	if err := ctrl.SetControllerReference(tj, obj, r.Scheme); err != nil {
		return err
	}
	if err := r.createResource(ctx, tj, obj); err != nil {
		return err
	}
	return nil
}

func (r *TJobReconciler) createResource(ctx context.Context, tj *tv1.TJob, obj client.Object) error {
	tp := obj.GetObjectKind().GroupVersionKind().Kind
	if err := r.Create(ctx, obj); err != nil {
		r.Recorder.Event(tj, corev1.EventTypeWarning, "Create", fmt.Sprintf("create failed %s %s", tp, obj.GetName()))
		return err
	}
	r.Recorder.Event(tj, corev1.EventTypeNormal, "Created", fmt.Sprintf("created %s %s", tp, obj.GetName()))
	return nil

}

func (r *TJobReconciler) execInPod(namespace string, podName string, containerName string, cmd []string) error {
	execReq := r.restClient.
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, runtime.NewParameterCodec(r.Scheme))

	exec, err := remotecommand.NewSPDYExecutor(r.restConfig, http.MethodPost, execReq.URL())
	if err != nil {
		return err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Tty:    false,
	})

	return err
}

func indexerFunc(rawObj client.Object) []string {
	owner := metav1.GetControllerOf(rawObj)
	if owner == nil {
		return nil
	}
	if owner.APIVersion != apiGVStr || owner.Kind != tv1.KIND {
		return nil
	}

	// ...and if so, return it
	return []string{owner.Name}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// restClient for exec
	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	restClient, err := apiutil.RESTClientForGVK(gvk, false, mgr.GetConfig(), serializer.NewCodecFactory(mgr.GetScheme()))
	if err != nil {
		return err
	}

	r.restClient = restClient
	r.restConfig = mgr.GetConfig()

	r.cache = ttlcache.NewCache(time.Second * 3)

	// index pod
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &corev1.Pod{}, ctrlRefKey, indexerFunc); err != nil {
		return err
	}

	// index service
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &corev1.Service{}, ctrlRefKey, indexerFunc); err != nil {
		return err
	}

	// index configmap
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &corev1.ConfigMap{}, ctrlRefKey, indexerFunc); err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&tv1.TJob{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{})

	return builder.Complete(r)
}
