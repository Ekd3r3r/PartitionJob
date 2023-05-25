package utils

import (
	"bytes"
	"context"
	"encoding/json"

	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	webappv1 "my.domain/partitionJob/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPartitionJob retrieves the current resource instance of PartitionJob
func GetPartitionJob(r client.Client, ctx context.Context, req ctrl.Request) (*webappv1.PartitionJob, error) {
	instance := &webappv1.PartitionJob{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		return nil, err
	}

	return instance, nil
}

// TemplateToRaw converts the input template to raw byte array
func TemplateToRaw(template *corev1.PodTemplateSpec) runtime.RawExtension {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	if err := enc.Encode(template); err != nil {
		panic(err)
	}
	return runtime.RawExtension{Raw: buf.Bytes()}
}

// RawToTemplate converts the input byte array to a PodTemplateSpec
func RawToTemplate(data []byte) *corev1.PodTemplateSpec {
	buf := bytes.NewBuffer(data)
	dec := json.NewDecoder(buf)

	var template corev1.PodTemplateSpec
	if err := dec.Decode(&template); err != nil {
		panic(err)
	}

	return &template
}

func GetRevisionPods(availableReplicas []*corev1.Pod, updatedRevision *apps.ControllerRevision, currentRevision *apps.ControllerRevision) ([]*corev1.Pod, []*corev1.Pod) {
	oldRevisionPods := make([]*corev1.Pod, 0)
	newRevisionPods := make([]*corev1.Pod, 0)

	for _, pod := range availableReplicas {

		podRevision := GetPodRevision(pod)

		if currentRevision != nil && podRevision == currentRevision.Name {
			oldRevisionPods = append(oldRevisionPods, pod)
		}
		if updatedRevision != nil && podRevision == updatedRevision.Name {
			newRevisionPods = append(newRevisionPods, pod)
		}
		//TODO
		// if previousRevision != nil && podRevision == previousRevision.Name {
		// 	r.Delete(ctx, pod)
		// 	numAvailableReplicas--
		// }
	}

	return oldRevisionPods, newRevisionPods
}
