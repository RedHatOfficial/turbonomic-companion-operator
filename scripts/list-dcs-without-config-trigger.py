#!/usr/bin/env python3

import json
import sys

try:
    data = json.load(sys.stdin)
except json.JSONDecodeError as e:
    print(f"❌ Failed to parse JSON from stdin: {e}", file=sys.stderr)
    sys.exit(1)

commands = []
for item in data.get("items", []):
    name = item["metadata"]["name"]
    ns = item["metadata"]["namespace"]
    triggers = item.get("spec", {}).get("triggers", [])
    annotations = item["metadata"].get("annotations", {})

    override = annotations.get("turbo.ibm.com/override", "false").lower() == "true"

    # Check if no configChange trigger is present
    has_config_trigger = any(
        t.get("type", "").lower() == "configchange" for t in triggers
    )

    if override and not has_config_trigger:
        commands.append(f"oc rollout latest dc/{name} -n {ns}")

for cmd in commands:
    print(cmd)
