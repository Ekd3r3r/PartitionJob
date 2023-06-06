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
	"reflect"

	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/controller/history"
	webappv1 "my.domain/partitionJob/api/v1"
	utils "my.domain/partitionJob/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PartitionJobReconciler reconciles a PartitionJob object
type PartitionJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	instance, err := utils.GetPartitionJob(r.Client, ctx, req)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	partitionJob := instance.DeepCopy()

	allRevisions, err := utils.ListRevisions(r.Client, ctx, partitionJob)
	if err != nil {
		return ctrl.Result{}, err
	}
	history.SortControllerRevisions(allRevisions)

	allRevisions, collisonCount, err := utils.GetAllRevisions(r.Client, ctx, partitionJob, allRevisions)
	if err != nil {
		return ctrl.Result{}, err
	}

	revisionCount := len(allRevisions)
	var currentRevision, updatedRevision *apps.ControllerRevision

	if revisionCount > 0 && allRevisions[revisionCount-1] != nil {
		//revision is sorted in ascending order, so the updated revision will be the last revision
		updatedRevision = allRevisions[revisionCount-1]
	}

	if revisionCount > 1 && allRevisions[revisionCount-2] != nil {
		//revision is sorted in ascending order, so the current revision will be the second to last revision
		currentRevision = allRevisions[revisionCount-2]
	}

	l.Info("Revision Info", "current revision:", currentRevision, "updated revision:", updatedRevision, "collision count:", collisonCount)

	// if currentRevision is not set because it is the first pass, set it equal to updatedRevision
	if currentRevision == nil {
		currentRevision = updatedRevision
	}

	availableReplicas, oldRevisionPods, newRevisionPods, err := utils.GetRevisionPods(r.Client, ctx, partitionJob, allRevisions)
	if err != nil {
		return ctrl.Result{}, err
	}
	numAvailableReplicas := int32(len(availableReplicas))

	//in the case partition is greater than desired replicas
	if *partitionJob.Spec.Partitions > partitionJob.Spec.Replicas {
		partitionJob.Spec.Partitions = &partitionJob.Spec.Replicas
	}

	observedStatus := webappv1.PartitionJobStatus{
		Replicas:          partitionJob.Spec.Replicas,  //desired replicas
		AvailableReplicas: numAvailableReplicas,        //observed replicas
		CurrentReplicas:   int32(len(oldRevisionPods)), //current replicas
		UpdatedReplicas:   int32(len(newRevisionPods)), //updated replicas
		CurrentRevision:   currentRevision.Name,        //current revision
		UpdateRevision:    updatedRevision.Name,        //update revision
	}

	if !reflect.DeepEqual(partitionJob.Status, observedStatus) {
		partitionJob.Status = observedStatus
		if err := r.Status().Update(ctx, partitionJob); err != nil {
			l.Error(err, "Failed to update PartitionJob")
			return ctrl.Result{}, err
		}
	}

	if numAvailableReplicas > partitionJob.Spec.Replicas {

		l.Info("Scaling down pods", "Currently available", numAvailableReplicas, "Required replicas", partitionJob.Spec.Replicas)
		numPodsToBeDeleted := numAvailableReplicas - partitionJob.Spec.Replicas
		diff := partitionJob.Status.UpdatedReplicas - *partitionJob.Spec.Partitions

		for i := 1; i <= int(numPodsToBeDeleted); i++ {
			var dpod *corev1.Pod
			if diff > 0 {
				dpod = newRevisionPods[len(newRevisionPods)-i]
				diff--
			} else {
				dpod = oldRevisionPods[len(oldRevisionPods)-i]
			}

			if err = r.Delete(ctx, dpod); err != nil {
				l.Error(err, "Failed to delete pod", "pod.name", dpod.Name)
				return ctrl.Result{}, err
			}

		}

		return ctrl.Result{}, nil
	}

	if numAvailableReplicas < partitionJob.Spec.Replicas {
		l.Info("Scaling up pods", "Currently available", numAvailableReplicas, "Required replicas", partitionJob.Spec.Replicas)
		diff := *partitionJob.Spec.Partitions - partitionJob.Status.UpdatedReplicas

		var pod *corev1.Pod
		if diff > 0 {
			//create pod with new revision
			template := utils.RawToTemplate(updatedRevision.Data.Raw)
			pod = utils.CreateNewPod(partitionJob, template)

			utils.SetPodRevision(pod, updatedRevision.Name)
		} else {
			//create pod with old revision
			template := utils.RawToTemplate(currentRevision.Data.Raw)
			pod = utils.CreateNewPod(partitionJob, template)

			utils.SetPodRevision(pod, currentRevision.Name)
		}

		// Set partitionJob instance as the owner and controller
		if err := controllerutil.SetControllerReference(partitionJob, pod, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		err = r.Create(ctx, pod)
		if err != nil {
			l.Error(err, "Failed to create pod", "pod.name", pod.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if currentRevision != updatedRevision && partitionJob.Status.UpdatedReplicas > *partitionJob.Spec.Partitions && *partitionJob.Spec.Partitions >= 0 {
		l.Info("Scaling down new revision pods", "Currently available", partitionJob.Status.UpdatedReplicas, "Required replicas", partitionJob.Spec.Partitions)
		diff := partitionJob.Status.UpdatedReplicas - *partitionJob.Spec.Partitions
		dpods := newRevisionPods[:diff]
		for _, dpod := range dpods {
			err = r.Delete(ctx, dpod)
			if err != nil {
				l.Error(err, "Failed to delete pod", "pod.name", dpod.Name)
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil

	} else if currentRevision != updatedRevision && partitionJob.Status.UpdatedReplicas < *partitionJob.Spec.Partitions {
		l.Info("Scaling up new revision pods", "Currently available", partitionJob.Status.UpdatedReplicas, "Required replicas", partitionJob.Spec.Partitions)
		diff := *partitionJob.Spec.Partitions - partitionJob.Status.UpdatedReplicas

		dpods := oldRevisionPods[:diff]
		for _, dpod := range dpods {
			err = r.Delete(ctx, dpod)
			if err != nil {
				l.Error(err, "Failed to delete pod", "pod.name", dpod.Name)
				return ctrl.Result{}, err
			}

			// Define a new Pod object
			template := utils.RawToTemplate(updatedRevision.Data.Raw)
			pod := utils.CreateNewPod(partitionJob, template)

			utils.SetPodRevision(pod, updatedRevision.Name)
			// Set partitionJob instance as the owner and controller
			if err := controllerutil.SetControllerReference(partitionJob, pod, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			err = r.Create(ctx, pod)
			if err != nil {
				l.Error(err, "Failed to create pod", "pod.name", pod.Name)
				return ctrl.Result{}, err
			}

		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *PartitionJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.PartitionJob{}).
		Owns(&corev1.Pod{}).
		Complete(r); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&apps.ControllerRevision{}).
		Complete(r)
}
