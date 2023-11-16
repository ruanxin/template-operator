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

REF_NAME="${2:-"main"}"
echo "REF_NAME $REF_NAME"
SHORT_EXPECTED_SHA=$(git rev-parse --short=8 "${REF_NAME}~")
echo "SHORT_EXPECTED_SHA $SHORT_EXPECTED_SHA"
DATE="v$(git show "${SHORT_EXPECTED_SHA}" --date=format:'%Y%m%d' --format=%ad -q)"
echo "DATE $DATE"
EXPECTED_VERSION="${DATE}-${SHORT_EXPECTED_SHA}"
echo "EXPECTED_VERSION $EXPECTED_VERSION"

IMAGE_TO_CHECK="${2:-europe-docker.pkg.dev/kyma-project/prod/template-operator}"
echo "IMAGE_TO_CHECK $IMAGE_TO_CHECK"
BUMPED_IMAGE_TAG=$(grep "${IMAGE_TO_CHECK}" ../../sec-scanners-config.yaml | cut -d : -f 2)
echo "BUMPED_IMAGE_TAG $BUMPED_IMAGE_TAG"

if [[ "$BUMPED_IMAGE_TAG" != "$EXPECTED_VERSION" ]]; then
  echo "Version tag in sec-scanners-config.yaml file is incorrect!"
  echo "Could not find $EXPECTED_VERSION."
  exit 1
fi
echo "Image version tag in sec-scanners-config.yaml does match with remote."
exit 0
