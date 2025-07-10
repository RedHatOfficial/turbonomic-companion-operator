#!/bin/bash

set -euo pipefail

NAMESPACE="${1:-}"
if [[ -z "$NAMESPACE" ]]; then
  echo "Usage: $0 <namespace>"
  exit 1
fi

KINDS=("deployment" "deploymentconfig" "statefulset")

echo "# Step 1: Set annotation to false if it was true"
for KIND in "${KINDS[@]}"; do
  oc get "$KIND" -n "$NAMESPACE" -o json | jq -r \
    --arg kind "$KIND" --arg ns "$NAMESPACE" \
    '.items[] 
     | select(.metadata.annotations["turbo.ibm.com/override"] == "true") 
     | "oc annotate \($kind) \(.metadata.name) -n \($ns) turbo.ibm.com/override=false --overwrite"' 
done

echo
echo "# Step 2: Remove the annotation"
for KIND in "${KINDS[@]}"; do
  oc get "$KIND" -n "$NAMESPACE" -o json | jq -r \
    --arg kind "$KIND" --arg ns "$NAMESPACE" \
    '.items[] 
     | select(.metadata.annotations["turbo.ibm.com/override"]) 
     | "oc annotate \($kind) \(.metadata.name) -n \($ns) turbo.ibm.com/override-"' 
done

