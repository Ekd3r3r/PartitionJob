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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	instance, err := r.GetPartitionJob(ctx, req)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	partitionJob := instance.DeepCopy()

	allRevisions, err := r.ListRevisions(ctx, partitionJob)
	if err != nil {
		return ctrl.Result{}, err
	}
	history.SortControllerRevisions(allRevisions)

	allRevisions, collisonCount, err := r.GetRevision(ctx, partitionJob, allRevisions)
	if err != nil {
		return ctrl.Result{}, err
	}

	revisionCount := len(allRevisions)
	var currentRevision, updatedRevision, previousRevision *apps.ControllerRevision

	if revisionCount > 0 && allRevisions[revisionCount-1] != nil {
		//revision is sorted in ascending order, so the updated revision will be the last revision
		updatedRevision = allRevisions[revisionCount-1]
	}

	if revisionCount > 1 && allRevisions[revisionCount-2] != nil {
		//revision is sorted in ascending order, so the current revision will be the second to last revision
		currentRevision = allRevisions[revisionCount-2]
	}

	if revisionCount > 2 && allRevisions[revisionCount-3] != nil {
		previousRevision = allRevisions[revisionCount-3]
	}

	l.Info("Revision Info", "current revision:", currentRevision, "updated revision:", updatedRevision, "collision count:", collisonCount)

	availableReplicas, err := r.GetAvailablePods(ctx, partitionJob)
	if err != nil {
		return ctrl.Result{}, err
	}
	numAvailableReplicas := int32(len(availableReplicas))

	oldRevisionPods := make([]*corev1.Pod, 0)
	newRevisionPods := make([]*corev1.Pod, 0)

	for _, pod := range availableReplicas {

		podRevision := utils.GetPodRevision(pod)

		if currentRevision != nil && podRevision == currentRevision.Name {
			oldRevisionPods = append(oldRevisionPods, pod)
		}
		if updatedRevision != nil && podRevision == updatedRevision.Name {
			newRevisionPods = append(newRevisionPods, pod)
		}
		if previousRevision != nil && podRevision == previousRevision.Name {
			r.Delete(ctx, pod)
			numAvailableReplicas--
		}
	}

	// if currentRevision is not set because it is the first pass, set it equal to updatedRevision
	if currentRevision == nil {
		currentRevision = updatedRevision
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
			l.Error(err, "Failed to update PartitionJob status")
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

	if currentRevision != updatedRevision && partitionJob.Status.UpdatedReplicas > *partitionJob.Spec.Partitions {
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

// GetPartitionJob retrieves the current resource instance of PartitionJob
func (r *PartitionJobReconciler) GetPartitionJob(ctx context.Context, req ctrl.Request) (*webappv1.PartitionJob, error) {
	instance := &webappv1.PartitionJob{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		return nil, err
	}

	return instance, nil
}

// GetAvailalePods returs an array of Pods that match the labels described in partitionJob spec's selector
func (r *PartitionJobReconciler) GetAvailablePods(ctx context.Context, partitionJob *webappv1.PartitionJob) ([]*corev1.Pod, error) {
	podList := &corev1.PodList{}
	labelsToMatch := partitionJob.Spec.Selector.MatchLabels
	labelSelector := labels.SelectorFromSet(labelsToMatch)

	listOptions := &client.ListOptions{Namespace: partitionJob.Namespace, LabelSelector: labelSelector}
	if err := r.List(context.TODO(), podList, listOptions); err != nil {
		return nil, err
	}

	// Count the pods that are pending or running as available
	availableReplicas := make([]*corev1.Pod, 0)
	for index, pod := range podList.Items {
		if pod.ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			availableReplicas = append(availableReplicas, &podList.Items[index])
		}
	}

	return availableReplicas, nil
}

// returns an array of ControllerRevisions with revisions of PartitionJob resource.
func (r *PartitionJobReconciler) ListRevisions(ctx context.Context, partitionJob *webappv1.PartitionJob) ([]*apps.ControllerRevision, error) {
	revisionList := &apps.ControllerRevisionList{}
	labelsToMatch := partitionJob.Spec.Selector.MatchLabels
	labelSelector := labels.SelectorFromSet(labelsToMatch)

	listOptions := &client.ListOptions{Namespace: partitionJob.Namespace, LabelSelector: labelSelector}

	if err := r.List(ctx, revisionList, listOptions); err != nil {
		return nil, err
	}

	allRevisions := make([]*apps.ControllerRevision, 0)
	for index := range revisionList.Items {
		allRevisions = append(allRevisions, &revisionList.Items[index])
	}

	return allRevisions, nil
}

func (r *PartitionJobReconciler) GetRevision(ctx context.Context, partitionJob *webappv1.PartitionJob, revisions []*apps.ControllerRevision) ([]*apps.ControllerRevision, int32, error) {
	var updatedRevision *apps.ControllerRevision
	var collisionCount int32 = 0

	revisionCount := len(revisions)
	history.SortControllerRevisions(revisions)

	updatedRevision, err := utils.CreateNewRevision(partitionJob, utils.GetNextRevision(revisions), &collisionCount)
	if err != nil {
		return nil, collisionCount, err
	}

	//find any equivalent revisions
	equivalentRevisions := history.FindEqualRevisions(revisions, updatedRevision)
	equivalentCount := len(equivalentRevisions)

	if equivalentCount > 0 && history.EqualRevision(revisions[revisionCount-1], equivalentRevisions[equivalentCount-1]) {
		//if the equivalent revision is the last updated revision, no need to do anything else
		return revisions, collisionCount, nil
	} else if equivalentCount > 0 {
		//get equivalent revision and increment the revision
		rev := equivalentRevisions[equivalentCount-1]
		rev.Revision = updatedRevision.Revision

		if err := r.Update(ctx, rev); err != nil {
			return nil, collisionCount, err
		}
	} else {
		//if there is no equivalent revision, we create one
		clone := updatedRevision.DeepCopy()
		clone.Namespace = partitionJob.GetNamespace()

		//only keep the 5 most recent revisions
		diff := revisionCount - 5
		if diff >= 0 {
			for i := 0; i < diff; i++ {
				rev := revisions[i]
				if err := r.Delete(ctx, rev); err != nil {
					return nil, collisionCount, err
				}
			}
		}

		if err := r.Create(ctx, clone); err != nil {
			if errors.IsAlreadyExists(err) {
				var cloneNamespacedName types.NamespacedName = types.NamespacedName{
					Namespace: clone.Namespace,
					Name:      clone.Name,
				}

				if err = r.Get(ctx, cloneNamespacedName, clone); err != nil {
					return nil, collisionCount, err
				}

				collisionCount++
			}
		}
	}

	updatedRevisions, _ := r.ListRevisions(ctx, partitionJob)
	history.SortControllerRevisions(updatedRevisions)

	return updatedRevisions, collisionCount, nil
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
