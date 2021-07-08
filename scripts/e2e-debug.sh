#!/bin/bash

exec dlv debug --api-version 2 --headless --listen 127.0.0.1:2345 ./cmd/chore/e2e/run/*.go -- "$@"
