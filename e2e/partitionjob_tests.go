package e2e

import (
	"context"
	"sort"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestPartitionJobs(t *testing.T) {

	// delete the namespace if it already exists
	cmd := kubectl("delete", "namespace", "partitionjob-test", "--ignore-not-found")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

	// create namespace
	cmd = kubectl("apply", "-f", "fixtures/namespace.yaml")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

	// Test PartitionJob Scaling
	cases := []struct {
		description     string
		fixtureFilePath string
	}{
		{
			description:     "Deploying PartitionJob",
			fixtureFilePath: "fixtures/partitionjob_deploy.yaml",
		},
		{
			description:     "Scaling up PartitionJob",
			fixtureFilePath: "fixtures/partitionjob_scale_up.yaml",
		},
		{
			description:     "Scaling down PartitionJob",
			fixtureFilePath: "fixtures/partitionjob_scale_down.yaml",
		},
	}

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for _, tc := range cases {

		t.Log(tc.description)

		cmd = kubectl("apply", "-f", tc.fixtureFilePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

		deadline, ok := t.Deadline()
		timeout := time.Until(deadline)
		if !ok {
			timeout = 300 * time.Second
		}

		cmd = kubectl("wait", "--for=condition=ready", "--timeout="+timeout.String(), "pods", "--selector=app=partitionjob-sample", "--namespace=partitionjob-test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

		cmd = kubectl("get", "partitionjob", "partitionjob-sample", "--namespace=partitionjob-test", "-o", "go-template={{.spec.replicas}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		expectedReplicas, err := strconv.Atoi(string(out))
		if err != nil {
			t.Fatal(err)
		}

		t.Log("Expected replicas: ", expectedReplicas)

		//wait for the pods to be scaled
		time.Sleep(45 * time.Second)

		actualReplicas := countPods(clientset, "status.phase=Running", "app=partitionjob-sample", "partitionjob-test")

		t.Log("Actual replicas: ", actualReplicas)

		if actualReplicas != expectedReplicas {
			t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
		}

		t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)

	}

	//Test Partitions, Pod Template change and Scaling
	cases = []struct {
		description     string
		fixtureFilePath string
	}{
		{
			description:     "Change Pod Template",
			fixtureFilePath: "fixtures/partitionjob_deploy_pod_template_change.yaml",
		},
		{
			description:     "Scaling up PartitionJob with Partition",
			fixtureFilePath: "fixtures/partitionjob_scale_up_with_partition.yaml",
		},
		{
			description:     "Scaling down PartitionJob with Partition",
			fixtureFilePath: "fixtures/partitionjob_scale_down_with_partition.yaml",
		},
		{
			description:     "Partition greater than number of replicas",
			fixtureFilePath: "fixtures/partitionjob_partition_greaterthan_replicas.yaml",
		},
	}

	for _, tc := range cases {

		t.Log(tc.description)

		cmd = kubectl("apply", "-f", tc.fixtureFilePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

		deadline, ok := t.Deadline()
		timeout := time.Until(deadline)
		if !ok {
			timeout = 300 * time.Second
		}

		cmd = kubectl("wait", "--for=condition=ready", "--timeout="+timeout.String(), "pods", "--selector=app=partitionjob-sample", "--namespace=partitionjob-test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

		cmd = kubectl("get", "partitionjob", "partitionjob-sample", "--namespace=partitionjob-test", "-o", "go-template={{.spec.replicas}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		expectedReplicas, err := strconv.Atoi(string(out))
		if err != nil {
			t.Fatal(err)
		}

		t.Log("Expected replicas: ", expectedReplicas)

		cmd = kubectl("get", "partitionjob", "partitionjob-sample", "--namespace=partitionjob-test", "-o", "go-template={{.spec.partitions}}")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		expectedPartitions, err := strconv.Atoi(string(out))
		if err != nil {
			t.Fatal(err)
		}

		t.Log("Expected partitions: ", expectedPartitions)

		//wait for the pods to be scaled
		time.Sleep(45 * time.Second)

		actualReplicas := countPods(clientset, "status.phase=Running", "app=partitionjob-sample", "partitionjob-test")

		t.Log("Actual replicas: ", actualReplicas)

		if actualReplicas != expectedReplicas {
			t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
		}

		t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)

		updateRevision := getUpdateRevision(clientset, "partitionjob-test")

		labelSelectors := map[string]string{
			"app":                       "partitionjob-sample",
			"PartitionJobRevisionLabel": updateRevision,
		}

		actualPartitions := countPods(clientset, "status.phase=Running", metav1.FormatLabelSelector(metav1.SetAsLabelSelector(labelSelectors)), "partitionjob-test")
		t.Log("Actual partitions: ", actualPartitions)

		if actualPartitions != expectedPartitions {
			t.Fatalf("Expected partitions %d, got %d", expectedPartitions, actualPartitions)
		}

		t.Logf("PartitionJob successfully created %d paritition pods", expectedPartitions)

	}

	t.Log("Cleaning up PartitionJob")
	cmd = kubectl("delete", "namespace", "partitionjob-test", "--ignore-not-found")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

}

func countPods(clientset *kubernetes.Clientset, fieldSelector string, labelSelectors string, namespace string) int {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelectors, FieldSelector: fieldSelector})
	if err != nil {
		panic(err.Error())
	}

	return len(pods.Items)
}

func getUpdateRevision(clientset *kubernetes.Clientset, namespace string) string {
	controllerRevisions, err := clientset.AppsV1().ControllerRevisions(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Sort the ControllerRevisions by revision number in descending order
	sort.SliceStable(controllerRevisions.Items, func(i, j int) bool {
		return controllerRevisions.Items[i].Revision > controllerRevisions.Items[j].Revision
	})

	// Get the ControllerRevision with the highest revision number
	return controllerRevisions.Items[0].Name

}
