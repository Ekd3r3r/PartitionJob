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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	webappv1 "my.domain/partitionJob/api/v1"
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
		pod := createNewPod(partitionJob)
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
func createNewPod(cr *webappv1.PartitionJob) *corev1.Pod {
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

// SetupWithManager sets up the controller with the Manager.
func (r *PartitionJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.PartitionJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
