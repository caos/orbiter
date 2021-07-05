#!/bin/bash

set -e

./scripts/build-debug-bins.sh
CGO_ENABLED=0 GOOS=linux go build -o ./artifacts/gen-charts  cmd/gen-charts/*.go
dlv exec --api-version 2 --headless --listen 127.0.0.1:2345 ./artifacts/orbctl-Darwin-x86_64 -- "$@"
