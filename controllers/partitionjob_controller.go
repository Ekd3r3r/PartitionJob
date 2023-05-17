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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/controller/history"
	webappv1 "my.domain/partitionJob/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PartitionJobReconciler reconciles a PartitionJob object
type PartitionJobReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	controllerHistory history.Interface
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

	instance := &webappv1.PartitionJob{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	partitionJob := instance
	podList := &corev1.PodList{}
	labelsToMatch := partitionJob.Spec.Selector.MatchLabels
	labelSelector := labels.SelectorFromSet(labelsToMatch)

	allRevisions, err := r.ListRevisions(partitionJob)
	if err != nil {
		return ctrl.Result{}, err
	}

	currentRevision, updatedRevision, collisonCount, err := r.GetRevision(partitionJob, allRevisions)
	if err != nil {
		return ctrl.Result{}, err
	}

	l.Info("Revision Info", currentRevision, updatedRevision, collisonCount)

	listOptions := &client.ListOptions{Namespace: partitionJob.Namespace, LabelSelector: labelSelector}
	if err = r.List(context.TODO(), podList, listOptions); err != nil {
		return ctrl.Result{}, err
	}

	// Count the pods that are pending or running as available
	var availableReplicas []corev1.Pod
	for _, pod := range podList.Items {
		if pod.ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			availableReplicas = append(availableReplicas, pod)
		}
	}
	numAvailableReplicas := int32(len(availableReplicas))

	observedStatus := webappv1.PartitionJobStatus{
		Replicas:        partitionJob.Spec.Replicas, //desired replicas
		CurrentReplicas: numAvailableReplicas,       //observed replicas
	}

	if !reflect.DeepEqual(partitionJob.Status, observedStatus) {
		partitionJob.Status = observedStatus
		if err := r.Status().Update(context.TODO(), partitionJob); err != nil {
			l.Error(err, "Failed to update PartitionJob status")
			return ctrl.Result{}, err
		}
	}

	if numAvailableReplicas > partitionJob.Spec.Replicas {
		l.Info("Scaling down pods", "Currently available", numAvailableReplicas, "Required replicas", partitionJob.Spec.Replicas)
		diff := numAvailableReplicas - partitionJob.Spec.Replicas
		dpods := availableReplicas[:diff]
		for _, dpod := range dpods {
			err = r.Delete(context.TODO(), &dpod)
			if err != nil {
				l.Error(err, "Failed to delete pod", "pod.name", dpod.Name)
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if numAvailableReplicas < partitionJob.Spec.Replicas {
		l.Info("Scaling up pods", "Currently available", numAvailableReplicas, "Required replicas", partitionJob.Spec.Replicas)
		// Define a new Pod object
		pod := CreateNewPod(partitionJob)
		// Set partitionJob instance as the owner and controller
		if err := controllerutil.SetControllerReference(partitionJob, pod, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		err = r.Create(context.TODO(), pod)
		if err != nil {
			l.Error(err, "Failed to create pod", "pod.name", pod.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// returns a pod with the same name/namespace as the CR, using the pod spec described in the CR
func CreateNewPod(cr *webappv1.PartitionJob) *corev1.Pod {
	labels := cr.Spec.Selector.MatchLabels
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-pod",
			Namespace:    cr.Namespace,
			Labels:       labels,
		},
		Spec: cr.Spec.Template.Spec,
	}
}

// returns an array of ControllerRevisions with revisions of PartitionJob resource.
func (r *PartitionJobReconciler) ListRevisions(partitionJob *webappv1.PartitionJob) ([]*apps.ControllerRevision, error) {
	selector, err := metav1.LabelSelectorAsSelector(partitionJob.Spec.Selector)
	if err != nil {
		return nil, err
	}

	return r.controllerHistory.ListControllerRevisions(partitionJob, selector)
}

// creates a new controller revision
func CreateNewRevision(partitionJob *webappv1.PartitionJob, revision int64, collisionCount *int32) (*apps.ControllerRevision, error) {
	cr, err := history.NewControllerRevision(partitionJob, partitionJob.GroupVersionKind(), partitionJob.Spec.Template.Labels, runtime.RawExtension{}, revision, collisionCount)

	if err != nil {
		return nil, err
	}

	if cr.ObjectMeta.Annotations == nil {
		cr.ObjectMeta.Annotations = make(map[string]string)
	}

	for key, value := range partitionJob.Annotations {
		cr.ObjectMeta.Annotations[key] = value
	}

	return cr, nil
}

// nextRevision finds the next valid revision number based on revisions. If the length of revisions
// is 0 this is 1. Otherwise, it is 1 greater than the largest revision's Revision. This method
// assumes that revisions has been sorted by Revision.
func GetNextRevision(revisions []*apps.ControllerRevision) int64 {
	count := len(revisions)
	if count <= 0 {
		return 1
	}
	return revisions[count-1].Revision + 1
}

func (r *PartitionJobReconciler) GetRevision(partitionJob *webappv1.PartitionJob, revisions []*apps.ControllerRevision) (*apps.ControllerRevision, *apps.ControllerRevision, int32, error) {
	var currentRevision, updatedRevision *apps.ControllerRevision

	//revisionCount := len(revisions)
	history.SortControllerRevisions(revisions)

	var collisionCount int32
	collisionCount = 0 //TODO

	updatedRevision, err := CreateNewRevision(partitionJob, GetNextRevision(revisions), &collisionCount)
	if err != nil {
		return nil, nil, collisionCount, err
	}

	//find any equivalent revisions
	equivalentRevisions := history.FindEqualRevisions(revisions, updatedRevision)
	equivalentCount := len(equivalentRevisions)

	if equivalentCount > 0 {
		//roll back by decrementing the revision of the equivalent revision
		updatedRevision, err = r.controllerHistory.UpdateControllerRevision(equivalentRevisions[equivalentCount-1], updatedRevision.Revision)
		if err != nil {
			return nil, nil, collisionCount, err
		}
	} else {
		//if there is no equivalent revision, we create one
		updatedRevision, err = r.controllerHistory.CreateControllerRevision(partitionJob, updatedRevision, &collisionCount)
		if err != nil {
			return nil, nil, collisionCount, err
		}
	}

	// attempt to find the revision that corresponds to the current revision
	for i := range revisions {
		if revisions[i].Name == partitionJob.Status.CurrentRevision {
			currentRevision = revisions[i]
			break
		}
	}

	// if the current revision is nil we initialize the history by setting it to the update revision
	if currentRevision == nil {
		currentRevision = updatedRevision
	}

	return currentRevision, updatedRevision, collisionCount, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PartitionJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.PartitionJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
