package utils

import (
	"bytes"
	"encoding/json"

	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/controller/history"
	webappv1 "my.domain/partitionJob/api/v1"
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

// creates a new controller revision
func CreateNewRevision(partitionJob *webappv1.PartitionJob, revision int64, collisionCount *int32) (*apps.ControllerRevision, error) {
	cr, err := history.NewControllerRevision(partitionJob, partitionJob.GroupVersionKind(), partitionJob.Spec.Selector.MatchLabels, TemplateToRaw(&partitionJob.Spec.Template), revision, collisionCount)

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
