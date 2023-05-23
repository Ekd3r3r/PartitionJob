package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPartitionJob(t *testing.T) {

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

		cmd = kubectl("get", "pods", "--field-selector=status.phase=Running", "--namespace=partitionjob-test", "--selector=app=partitionjob-sample", "--no-headers", "-o", "name")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		actualReplicas := len(strings.Fields(string(out)))

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
			description:     "Partition == 0",
			fixtureFilePath: "fixtures/partitionjob_partition_eq_zero.yaml",
		},
		{
			description:     "Partition < 0",
			fixtureFilePath: "fixtures/partitionjob_partition_smallerthan_zero.yaml",
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

		cmd = kubectl("get", "pods", "--field-selector=status.phase=Running", "--namespace=partitionjob-test", "--selector=app=partitionjob-sample", "--no-headers", "-o", "name")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		actualReplicas := len(strings.Fields(string(out)))

		t.Log("Actual replicas: ", actualReplicas)

		if actualReplicas != expectedReplicas {
			t.Fatalf("Expected replicas %d, got %d", expectedReplicas, actualReplicas)
		}

		t.Logf("PartitionJob successfully created %d pod replicas", expectedReplicas)

		cmd = kubectl("get", "pods", "--field-selector=status.phase=Running", "--namespace=partitionjob-test", "--selector=app=partitionjob-sample", "--no-headers", "-o", "name")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		actualPartitions := len(strings.Fields(string(out)))

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
