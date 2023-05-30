package e2e

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
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

	// Path to your kubeconfig file
	kubeconfig := flag.String("kubeconfig", filepath.Join(os.Getenv("HOME"), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flag.Parse()

	// BuildConfigFromFlags creates a Kubernetes REST client configuration
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	client, err := ctrl.New(config, ctrl.Options{Scheme: scheme.Scheme})

	//Test Partitions, Pod Template change and Scaling
	cases := []struct {
		description     string
		fixtureFilePath string
		testPartitions  bool
	}{
		{
			description:     "Deploying PartitionJob",
			fixtureFilePath: "fixtures/partitionjob_deploy.yaml",
			testPartitions:  false,
		},
		{
			description:     "Scaling up PartitionJob",
			fixtureFilePath: "fixtures/partitionjob_scale_up.yaml",
			testPartitions:  false,
		},
		{
			description:     "Scaling down PartitionJob",
			fixtureFilePath: "fixtures/partitionjob_scale_down.yaml",
			testPartitions:  false,
		},
		{
			description:     "Change Pod Template",
			fixtureFilePath: "fixtures/partitionjob_deploy_pod_template_change.yaml",
			testPartitions:  true,
		},
		{
			description:     "Scaling up PartitionJob with Partition",
			fixtureFilePath: "fixtures/partitionjob_scale_up_with_partition.yaml",
			testPartitions:  true,
		},
		{
			description:     "Scaling down PartitionJob with Partition",
			fixtureFilePath: "fixtures/partitionjob_scale_down_with_partition.yaml",
			testPartitions:  true,
		},
		// {
		// 	description:     "Partition greater than number of replicas",
		// 	fixtureFilePath: "fixtures/partitionjob_scale_partition_greaterthan_replicas.yaml",
		// testPartitions: true,
		// },
	}

	for _, tc := range cases {

		t.Log(tc.description)

		cmd = kubectl("apply", "-f", tc.fixtureFilePath)
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

		var expectedPartitions int

		if tc.testPartitions {
			cmd = kubectl("get", "partitionjob", "partitionjob-sample", "--namespace=partitionjob-test", "-o", "go-template={{.spec.partitions}}")
			out, err = cmd.CombinedOutput()
			if err != nil {
				t.Fatal(string(out))
			}

			expectedPartitions, err = strconv.Atoi(string(out))
			if err != nil {
				t.Fatal(err)
			}

			t.Log("Expected partitions: ", expectedPartitions)
		}

		timeout := 5 * time.Minute

		ticker := time.NewTicker(10 * time.Second)

		defer ticker.Stop()

		var actualReplicas int

		conditionMet := false

		for !conditionMet {
			select {
			case <-ticker.C:
				actualReplicas = countPods(clientset, "status.phase=Running", "app=partitionjob-sample", "partitionjob-test")
				conditionMet = actualReplicas == expectedReplicas
				if conditionMet {
					t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)

				}

			case <-time.After(timeout):
				t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
			}
		}

		if tc.testPartitions {

			updateRevision := getUpdateRevision(clientset, "partitionjob-test")

			labelSelectors := map[string]string{
				"app":                       "partitionjob-sample",
				"PartitionJobRevisionLabel": updateRevision,
			}

			for !conditionMet {
				select {
				case <-ticker.C:
					actualPartitions := countPods(clientset, "status.phase=Running", metav1.FormatLabelSelector(metav1.SetAsLabelSelector(labelSelectors)), "partitionjob-test")
					t.Log("Actual partitions: ", actualPartitions)

					conditionMet = actualReplicas == expectedReplicas
					if conditionMet {
						t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)

					}

				case <-time.After(timeout):
					t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
				}
			}

			actualPartitions := countPods(clientset, "status.phase=Running", metav1.FormatLabelSelector(metav1.SetAsLabelSelector(labelSelectors)), "partitionjob-test")
			t.Log("Actual partitions: ", actualPartitions)

			if actualPartitions != expectedPartitions {
				t.Fatalf("Expected partitions %d, got %d", expectedPartitions, actualPartitions)
			}

			t.Logf("PartitionJob successfully created %d paritition pods", expectedPartitions)

		}

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
