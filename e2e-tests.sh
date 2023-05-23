#!/bin/bash

# Run the make commands
make unit-test
make docker-build docker push IMG=1d3r3r/partitionjob:latest
make deploy IMG=1d3r3r/partitionjob:latest
make e2e-test

