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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"
	webappv1 "my.domain/partitionJob/api/v1"
)

// PartitionJobReconciler reconciles a PartitionJob object
type PartitionJobReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	partitionJob *webappv1.PartitionJob
}

//+kubebuilder:rbac:groups=webapp.my.domain,resources=partitionjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=webapp.my.domain,resources=partitionjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=webapp.my.domain,resources=partitionjobs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PartitionJob object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *PartitionJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	partitionJob, err := r.GetPartitionJob(ctx, req)

	if err != nil {
		pod, err := r.GetPod(ctx, req)

		if pod.Labels["app"] == "for-partition" {
			l.Info("Reconciling Pod", "Pod", pod)
			//for number of partitions defined in our partition job
			//r.update pod using pod spec in our partition job template
		}

		if err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	} else {
		l.Info("Reconciling partition", "partition job info", partitionJob)
		r.partitionJob = partitionJob

		podList := &corev1.PodList{}
		pods := []corev1.Pod{}

		if err := r.List(ctx, podList); err != nil {
			l.Error(err, "no pods found")
		} else {
			for _, pod := range podList.Items {
				if pod.Labels["app"] == "for-partition" {
					pods = append(pods, pod)
				}
			}

			replicas := int32(len(pods))
			r.partitionJob.Spec.Replicas = &replicas
			l.Info("Partition job updated", "Spec", r.partitionJob.Spec)
		}

	}

	return ctrl.Result{}, nil
}

func (r *PartitionJobReconciler) GetPartitionJob(ctx context.Context, req ctrl.Request) (*webappv1.PartitionJob, error) {
	partition := &webappv1.PartitionJob{} //try to parse req into partitionJob resource

	if err := r.Get(ctx, req.NamespacedName, partition); err != nil {
		return nil, err
	}

	return partition, nil
}

func (r *PartitionJobReconciler) GetPod(ctx context.Context, req ctrl.Request) (*corev1.Pod, error) {
	pod := &corev1.Pod{} //try to parse req into pod resource

	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		return nil, err
	}

	return pod, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PartitionJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.PartitionJob{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
