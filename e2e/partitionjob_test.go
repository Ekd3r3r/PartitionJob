package e2e

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/pkg/controller/history"
	webappv1 "my.domain/partitionJob/api/v1"
	utils "my.domain/partitionJob/utils"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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

	kubeconfig := flag.Lookup("kubeconfig")

	if kubeconfig == nil {
		flag.String("kubeconfig", filepath.Join(os.Getenv("HOME"), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig.Value.Set(filepath.Join(os.Getenv("HOME"), ".kube", "config"))
	}

	// BuildConfigFromFlags creates a Kubernetes REST client configuration
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig.Value.String())
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(webappv1.AddToScheme(scheme))

	client, err := ctrl.New(config, ctrl.Options{Scheme: scheme})

	if err != nil {
		panic(err.Error())
	}

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

		var partitionJob *webappv1.PartitionJob

		timeout := 5 * time.Minute

		ticker := time.NewTicker(10 * time.Second)

		defer ticker.Stop()

		conditionMet := false

		for !conditionMet {
			select {
			case <-ticker.C:
				partitionJob, _ = utils.GetPartitionJob(client, context.TODO(), reconcile.Request{NamespacedName: types.NamespacedName{Name: "partitionjob-sample", Namespace: "partitionjob-test"}})
				conditionMet = partitionJob != nil
				if conditionMet {
					t.Logf("PartitionJob %s is successfully created", partitionJob.Name)

				}

			case <-time.After(timeout):
				t.Fatalf("Cannot create PartitionJob %s ", partitionJob.Name)
			}
		}

		var allRevisions []*apps.ControllerRevision

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

			allRevisions, _ = utils.ListRevisions(client, context.TODO(), partitionJob)

			history.SortControllerRevisions(allRevisions)

			allRevisions, _, _ = utils.GetAllRevisions(client, context.TODO(), partitionJob, allRevisions)
		}

		var actualReplicas int
		var availableReplicas []*corev1.Pod

		for !conditionMet {
			select {
			case <-ticker.C:
				availableReplicas, _ = utils.GetAvailablePods(client, context.Background(), partitionJob)
				actualReplicas = len(availableReplicas)
				conditionMet = actualReplicas == expectedReplicas
				if conditionMet {
					t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)

				}

			case <-time.After(timeout):
				t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
			}
		}

		if tc.testPartitions {

			var actualPartitions int

			for !conditionMet {
				select {
				case <-ticker.C:
					_, _, newRevisionPods, _ := utils.GetRevisionPods(client, context.Background(), partitionJob, allRevisions)
					actualPartitions = len(newRevisionPods)
					t.Log("Actual partitions: ", actualPartitions)

					conditionMet = actualPartitions == expectedPartitions
					if conditionMet {
						t.Logf("PartitionJob successfully created %d paritition pods", expectedPartitions)

					}

				case <-time.After(timeout):
					t.Fatalf("Expected partitions %d, got %d", expectedPartitions, actualPartitions)
				}
			}

		}

	}

	t.Log("Cleaning up PartitionJob")
	cmd = kubectl("delete", "namespace", "partitionjob-test", "--ignore-not-found")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

}
