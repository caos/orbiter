#!/usr/bin/env bash

set -e
[[ `uname` = "Linux" ]] && ENCODE="base64 --wrap=0" || ENCODE="base64"

cat <<EOL
apiVersion: v1
data:
  username: $(echo -n "${1}" | $ENCODE)
  password: $(echo -n "${2}" | $ENCODE)
kind: Secret
metadata:
  name: loki-grafana-cloud
  namespace: caos-system
type: Opaque
---
EOL
