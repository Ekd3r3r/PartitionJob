package utils

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	webappv1 "my.domain/partitionJob/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPodRevision gets the revision of Pod by inspecting the PartitionJobRevisionLabel. If pod has no revision the empty
// string is returned.
func GetPodRevision(pod *corev1.Pod) string {
	if pod.Labels == nil {
		return ""
	}

	return pod.Labels["PartitionJobRevisionLabel"]
}

// SetPodRevision sets the revision of Pod to revision by adding the StatefulSetRevisionLabel
func SetPodRevision(pod *corev1.Pod, revision string) {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels["PartitionJobRevisionLabel"] = revision
}

// CreateNewPod returns a pod with the same name/namespace as the CR, using the pod spec described in the CR
func CreateNewPod(cr *webappv1.PartitionJob, template *corev1.PodTemplateSpec) *corev1.Pod {
	labels := cr.Spec.Selector.MatchLabels
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-pod",
			Namespace:    cr.Namespace,
			Labels:       labels,
		},
		Spec: template.Spec,
	}
}

// GetAvailalePods returs an array of Pods that match the labels described in partitionJob spec's selector
func GetAvailablePods(r client.Client, ctx context.Context, partitionJob *webappv1.PartitionJob) ([]*corev1.Pod, error) {
	podList := &corev1.PodList{}
	labelsToMatch := partitionJob.Spec.Selector.MatchLabels
	labelSelector := labels.SelectorFromSet(labelsToMatch)

	listOptions := &client.ListOptions{Namespace: partitionJob.Namespace, LabelSelector: labelSelector}
	if err := r.List(ctx, podList, listOptions); err != nil {
		return nil, err
	}

	// Count the pods that are pending or running as available
	availableReplicas := make([]*corev1.Pod, 0)
	for index, pod := range podList.Items {
		if pod.ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning {
			availableReplicas = append(availableReplicas, &podList.Items[index])
		}
	}

	return availableReplicas, nil
}
