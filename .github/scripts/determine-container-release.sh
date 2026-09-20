#!/usr/bin/env bash

set -euo pipefail

VERSION="$(make version)"
PUBLISH=false
PUBLISH_LATEST=false
IMAGE_TAG="${VERSION}"

if [[ ! "${VERSION}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  echo "::error::Project version '${VERSION}' is not a valid container tag."
  exit 1
fi

if [[ "${EVENT_NAME}" == "push" && "${REF}" == "refs/heads/main" ]]; then
  PUBLISH=true
  PUBLISH_LATEST=true
elif [[ "${EVENT_NAME}" == "workflow_dispatch" && "${REF}" == "refs/heads/main" ]]; then
  PUBLISH=true
  PUBLISH_LATEST=true
elif [[ "${EVENT_NAME}" == "pull_request" && "${HEAD_REPOSITORY:-}" == "${REPOSITORY}" ]]; then
  if [[ ! "${PR_NUMBER:-}" =~ ^[1-9][0-9]*$ ]]; then
    echo "::error::A pull request number is required to publish a pull request image."
    exit 1
  fi

  if [[ ! "${COMMIT_SHA:-}" =~ ^[0-9a-f]{40}$ ]]; then
    echo "::error::The pull request commit SHA must be a full Git SHA."
    exit 1
  fi

  PUBLISH=true
  IMAGE_TAG="pr-${PR_NUMBER}-${COMMIT_SHA:0:12}"
fi

{
  echo "version=${VERSION}"
  echo "image_tag=${IMAGE_TAG}"
  echo "publish=${PUBLISH}"
  echo "publish_latest=${PUBLISH_LATEST}"
} >> "${GITHUB_OUTPUT}"
