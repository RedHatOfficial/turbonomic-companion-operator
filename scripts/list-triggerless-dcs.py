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
    triggers = item.get("spec", {}).get("triggers", None)
    annotations = item["metadata"].get("annotations", {})

    override = annotations.get("turbo.ibm.com/override", "false").lower() == "true"

    if override and (triggers is None or (isinstance(triggers, list) and len(triggers) == 0)):
        commands.append(f"oc rollout latest dc/{name} -n {ns}")

for cmd in commands:
    print(cmd)
