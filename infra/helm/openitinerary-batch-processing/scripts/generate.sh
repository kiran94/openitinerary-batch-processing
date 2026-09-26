#!/bin/bash

set -euo pipefail

# Assumed to be running in the ./infra/helm/ directory

# Generate the otel configuration into a cm
OUTPUT=./openitinerary-batch-processing/templates/configmap-otel-collector.yaml

{
    echo '{{- if .Values.telemetry.enabled }}'

    kubectl create configmap configmap-otel \
        --from-file=config.yaml=../otel-collector-config-cloud.yaml \
        --dry-run=client \
        --namespace '{{ .Release.Namespace }}' \
        -o yaml

    echo '{{- end }}'
} > "$OUTPUT"

echo "generated to $OUTPUT"
