#!/usr/bin/env bash

set -euo pipefail

make proto_generate

generated_status="$(git status --short --untracked-files=all -- proto/gen/go)"
if [[ -z "${generated_status}" ]]; then
  exit 0
fi

echo "::error::Generated protobuf code is out of date. Run 'make proto_generate' and commit the result."
git diff -- proto/gen/go
printf '%s\n' "${generated_status}"
exit 1
