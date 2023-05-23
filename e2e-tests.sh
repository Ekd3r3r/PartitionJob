#!/bin/bash

# Run the make commands
make docker-build docker push IMG=1d3r3r/partitionjob:latest
make deploy IMG=1d3r3r/partitionjob:latest
go test ./e2e -v -count=1
make uninstall
make undeploy

