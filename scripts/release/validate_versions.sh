#!/usr/bin/env bash

set -ue
source ./../../.version

DESIRED_VERSION=$1
if [[ "$DESIRED_VERSION" != "$MODULE_VERSION" ]]; then
  echo "Versions don't match! Expected ${MODULE_VERSION} but got $DESIRED_VERSION."
  echo "Please update .version file or change desired version!"
  exit 1
fi
echo "Versions match."

IMAGE_TO_CHECK="${2:-europe-docker.pkg.dev/kyma-project/prod/template-operator}"
BUMPED_IMAGE_TAG=$(grep "${IMAGE_TO_CHECK}" ../../sec-scanners-config.yaml | cut -d : -f 2)
if [[ "$BUMPED_IMAGE_TAG" != "$DESIRED_VERSION" ]]; then
  echo "Version tag in sec-scanners-config.yaml file is incorrect!"
  echo "Could not find $DESIRED_VERSION."
  exit 1
fi
echo "Image version tag in sec-scanners-config.yaml does match with release tag."
exit 0
