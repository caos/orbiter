#!/bin/bash

set -ex

KEY=$1
DEST="orbiter@$2"
shift
shift

scp -i $KEY ./scripts/stop-na.sh $DEST:/usr/local/bin/stop-na.sh
ssh -i $KEY $DEST stop-na.sh
go run ./cmd/chore/dev/executables/*.go
scp -i $KEY ./artifacts/nodeagent $DEST:/usr/local/bin/node-agent
scp -i $KEY ./scripts/debug-na.sh $DEST:/usr/local/bin/debug-na.sh
ssh -i $KEY $DEST debug-na.sh "$@"

