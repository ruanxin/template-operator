#!/usr/bin/env bash

echo "Status of all 'post-*' pipelines for template-operator"

REF_NAME="${1:-"main"}"
STATUS_URL="https://api.github.com/repos/kyma-project/template-operator/commits/${REF_NAME}/status"
STATUS=$(curl -L -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "${STATUS_URL}" | head -n 2 )

sleep 10
echo "$STATUS"

if [[ "$STATUS" == *"success"* ]]; then
  echo "All jobs succeeded"
else
  echo "Some jobs failed or pending!"
  exit 1
fi
