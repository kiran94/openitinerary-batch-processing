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

make docker_images_build_service docker_images_build_worker

SERVICE_REPOSITORY="${REGISTRY}/${REPOSITORY}"
WORKER_REPOSITORY="${REGISTRY}/${REPOSITORY}-worker"

docker tag "oibp:${VERSION}" "${SERVICE_REPOSITORY}:${VERSION}"
docker tag "oibp-worker:${VERSION}" "${WORKER_REPOSITORY}:${VERSION}"

if [[ "${PUBLISH}" != "true" ]]; then
  exit 0
fi

docker tag "oibp:${VERSION}" "${SERVICE_REPOSITORY}:latest"
docker tag "oibp-worker:${VERSION}" "${WORKER_REPOSITORY}:latest"

docker push "${SERVICE_REPOSITORY}:${VERSION}"
docker push "${SERVICE_REPOSITORY}:latest"
docker push "${WORKER_REPOSITORY}:${VERSION}"
docker push "${WORKER_REPOSITORY}:latest"
