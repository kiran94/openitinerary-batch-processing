#!/usr/bin/env bash

set -euo pipefail

: "${VERSION:?VERSION must be set}"
: "${REGISTRY:?REGISTRY must be set}"
: "${REPOSITORY:?REPOSITORY must be set}"
: "${PUBLISH:?PUBLISH must be set}"

if [[ "${PUBLISH}" != "true" && "${PUBLISH}" != "false" ]]; then
  echo "::error::PUBLISH must be either 'true' or 'false'."
  exit 1
fi

MAKE_VERSION="$(make version)"
if [[ "${MAKE_VERSION}" != "${VERSION}" ]]; then
  echo "::error::Expected container version '${VERSION}', but Makefile resolved '${MAKE_VERSION}'."
  exit 1
fi

SERVICE_REPOSITORY="${REGISTRY}/${REPOSITORY}"
WORKER_REPOSITORY="${REGISTRY}/${REPOSITORY}-worker"

if [[ "${PUBLISH}" != "true" ]]; then
  make docker_images_build_service docker_images_build_worker

  docker tag "oibp:${VERSION}" "${SERVICE_REPOSITORY}:${VERSION}"
  docker tag "oibp-worker:${VERSION}" "${WORKER_REPOSITORY}:${VERSION}"

  exit 0
fi

make \
  SERVICE_IMAGE="${SERVICE_REPOSITORY}" \
  WORKER_IMAGE="${WORKER_REPOSITORY}" \
  docker_images_publish_service \
  docker_images_publish_worker
