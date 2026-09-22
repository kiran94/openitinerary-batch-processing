#!/bin/bash

set -e

# Assumed to be running in the ./infra/k8s/ directory

# Generate the otel configuration into a cm
OUTPUT=configmap-otel-collector.yaml
kubectl create configmap configmap-otel \
    --from-file=config.yaml=../otel-collector-config-cloud.yaml \
    --dry-run=client \
    --namespace oibp \
    -o yaml > configmap-otel-collector.yaml

echo "generated to $OUTPUT"
