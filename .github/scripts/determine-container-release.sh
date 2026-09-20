#!/usr/bin/env bash

set -euo pipefail

VERSION="$(make version)"
PUBLISH=false

if [[ ! "${VERSION}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  echo "::error::Project version '${VERSION}' is not a valid container tag."
  exit 1
fi

if [[ "${EVENT_NAME}" == "push" && "${REF}" == "refs/heads/main" ]]; then
  PUBLISH=true
elif [[ "${EVENT_NAME}" == "workflow_dispatch" && "${REF}" == "refs/heads/main" ]]; then
  PUBLISH=true
fi

{
  echo "version=${VERSION}"
  echo "publish=${PUBLISH}"
} >> "${GITHUB_OUTPUT}"
