package e2e

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	webappv1 "my.domain/partitionJob/api/v1"
	utils "my.domain/partitionJob/utils"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestPartitionJobs(t *testing.T) {

	kubeconfig := flag.Lookup("kubeconfig")

	var kubeConfigValue string

	if kubeconfig == nil {
		kubeConfigValue = *flag.String("kubeconfig", filepath.Join(os.Getenv("HOME"), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		flag.Parse()
	} else {
		kubeconfig.Value.Set(filepath.Join(os.Getenv("HOME"), ".kube", "config"))
		kubeConfigValue = kubeconfig.Value.String()
	}

	// BuildConfigFromFlags creates a Kubernetes REST client configuration
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigValue)
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)

		if err != nil {
			log.Fatalf("Error building kubeconfig: %v", err)
		}

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
			description:     "Scaling up PartitionJob with Partition",
			fixtureFilePath: "fixtures/partitionjob_scale_up_with_partition.yaml",
			testPartitions:  true,
		},
		{
			description:     "Scaling down PartitionJob with Partition",
			fixtureFilePath: "fixtures/partitionjob_scale_down_with_partition.yaml",
			testPartitions:  true,
		},
		{
			description:     "Partition greater than number of replicas",
			fixtureFilePath: "fixtures/partitionjob_scale_partition_greaterthan_replicas.yaml",
			testPartitions:  true,
		},
	}

	for _, tc := range cases {

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

		//If we want to test partitions, initially deploy PartiitionJob with a different pod template
		//so that we can have two revisions for testing
		if tc.testPartitions {
			cmd = kubectl("apply", "-f", "fixtures/partitionjob_pod_template_change.yaml")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatal(string(out))
			}
		} else {
			cmd = kubectl("apply", "-f", "fixtures/partitionjob_deploy.yaml")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatal(string(out))
			}
		}

		var partitionJob *webappv1.PartitionJob

		setupBackoff := wait.Backoff{
			Steps:    50,
			Duration: time.Second * 10,
			Factor:   0,
			Jitter:   0.1,
		}

		err = retry.OnError(setupBackoff,
			func(err error) bool {
				return true
			}, func() error {
				partitionJob, err = utils.GetPartitionJob(client, context.Background(), types.NamespacedName{Name: "partitionjob-sample", Namespace: "partitionjob-test"})
				if err != nil {
					t.Logf("Unable to obtain PartitionJob resource %s. Retrying", partitionJob.Name)
					return err
				}
				t.Logf("PartitionJob %s is successfully created", partitionJob.Name)
				return nil
			})

		if err != nil {
			t.Fatalf("Cannot create PartitionJob %s ", partitionJob.Name)
		}

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

		err = retry.OnError(setupBackoff,
			func(err error) bool {
				return true
			}, func() error {
				partitionJob, err = utils.GetPartitionJob(client, context.Background(), types.NamespacedName{Name: "partitionjob-sample", Namespace: "partitionjob-test"})
				if err != nil {
					t.Logf("Unable to obtain PartitionJob resource %s. Retrying", partitionJob.Name)
					return err
				}
				t.Logf("PartitionJob %s is successfully created", partitionJob.Name)
				return nil
			})

		if err != nil {
			t.Fatalf("Cannot create PartitionJob %s ", partitionJob.Name)
		}

		if tc.testPartitions {
			cmd = kubectl("get", "partitionjob", "partitionjob-sample", "--namespace=partitionjob-test", "-o", "go-template={{.spec.partitions}}")
			out, err = cmd.CombinedOutput()
			if err != nil {
				t.Fatal(string(out))
			}

			expectedPartitions, err = strconv.Atoi(string(out))
			if expectedPartitions > expectedReplicas {
				expectedPartitions = expectedReplicas
			}
			if err != nil {
				t.Fatal(err)
			}

			t.Log("Expected partitions: ", expectedPartitions)

		}

		var actualReplicas int
		var availableReplicas []*corev1.Pod

		err = retry.OnError(setupBackoff,
			func(err error) bool {
				return true
			}, func() error {

				_, _, availableReplicas, _, _, err = utils.GetRevisionsPods(client, context.Background(), partitionJob)
				if err != nil {
					t.Logf("Unable to obtain Available Replicas. Retrying")
					return err
				} else if len(availableReplicas) != expectedReplicas {
					t.Logf("Expected replicas %d, got %d. Retrying", expectedReplicas, len(availableReplicas))
					return errors.New("replicas not ready")
				}

				t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)
				return nil
			})

		if err != nil {
			t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
		}

		if tc.testPartitions {
			var newRevisionPods []*corev1.Pod
			var actualPartitions int

			err = retry.OnError(setupBackoff,
				func(err error) bool {
					return true
				}, func() error {

					_, _, _, _, newRevisionPods, err = utils.GetRevisionsPods(client, context.Background(), partitionJob)
					actualPartitions = len(newRevisionPods)

					if err != nil {
						t.Logf("Unable to obtain partition pods. Retrying")
						return err
					} else if actualPartitions != expectedPartitions {
						t.Logf("Expected partitions %d, got %d. Retrying", expectedPartitions, actualPartitions)
						return errors.New("partitions not ready")
					}

					t.Logf("PartitionJob successfully created %d paritition pods", expectedPartitions)
					return nil
				})

			if err != nil {
				t.Fatalf("Expected partitions %d, got %d", expectedPartitions, actualPartitions)
			}
		}

		t.Log("Cleaning up PartitionJob")

		cmd = kubectl("delete", "partitionjob", "partitionjob-sample", "-n", "partitionjob-test", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

		cmd = kubectl("delete", "all", "--all", "-n", "partitionjob-test", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

		cmd = kubectl("delete", "namespace", "partitionjob-test", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}

	}

}
