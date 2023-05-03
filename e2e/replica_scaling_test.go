package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPartitionJobReplicaScaling(t *testing.T) {

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

	t.Log("Cleaning up PartitionJob")
	cmd = kubectl("delete", "namespace", "partitionjob-test", "--ignore-not-found")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

}
