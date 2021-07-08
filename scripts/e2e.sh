#!/bin/bash

exec go run ./cmd/chore/e2e/run/*.go "$@"
