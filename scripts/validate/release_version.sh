#!/usr/bin/env bash

set -ue
source ../../.version

DESIRED_VERSION=$1
if [[ "$DESIRED_VERSION" != "$MODULE_VERSION" ]]; then
  echo "Versions don't match! Expected ${MODULE_VERSION} but got $DESIRED_VERSION."
  echo "Please update .version file or change desired version!"
  exit 1
fi
echo "Versions match."

exit 0
