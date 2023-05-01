package e2e

import (
	"strconv"
	"testing"
	"time"
)

func TestPartitionJobReplicaScaling(t *testing.T) {

	// delete the namespace if it already exists
	cmd := kubectl("delete", "namespace", "partionjob-test", "--ignore-not-found")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

	// create namespace
	cmd = kubectl("apply", "-f", "./e2e/fixtures/namespace.yml")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}

	cases := []struct {
		description     string
		fixtureFilePath string
	}{
		{
			description:     "Deploying PartitionJob",
			fixtureFilePath: "./e2e/fixtures/partitionjob_deploy.yml",
		},
		{
			description:     "Scaling up PartitionJob",
			fixtureFilePath: "./e2e/fixtures/partitionjob_scale_up.yml",
		},
		{
			description:     "Scaling down PartitionJob",
			fixtureFilePath: "./e2e/fixtures/partitionjob_scale_down.yml",
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

		cmd = kubectl("wait", "--for=condition=ready", "--timeout="+timeout.String(), "pod", "-selector=app=partitionjob-sample", "--namespace=partitionjob-test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		t.Log("Success creating PartitionJob")

		cmd = kubectl("get", "pods", "-selector=app=partitionjob-sample", "--namespace=partitionjob-test", "--no-headers", "| wc -l")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		expectedReplicas, err := strconv.Atoi(string(out))
		if err != nil {
			t.Fatal(err)
		}

		cmd = kubectl("get", "partitionjob", "partitionjob-sample", "--namespace=partitionjob-test", "-o 'go-template={{.spec.replicas}}'")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}

		actualReplicas, err := strconv.Atoi(string(out))
		if err != nil {
			t.Fatal(err)
		}

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
