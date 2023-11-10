#!/usr/bin/env bash

set -ue
source .version

DESIRED_TAG=$1

if [[ "$DESIRED_TAG" != "$MODULE_VERSION" ]]; then
  echo "Tags mismatch: expected ${MODULE_VERSION}, got $DESIRED_TAG"
  exit 1
fi
echo "Tags match"
exit 0
