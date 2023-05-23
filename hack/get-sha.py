#/bin/python3

# Return sha's for every arch in a manifest

# Input: stdin raw json docker manifest
# Output: 
#   arm64=sha:for-arm
#   amd64=sha:for-amd

import sys, json

manifest_list = json.load(sys.stdin)["manifests"]
for m in manifest_list:
    # skip attestation manifests
    if m.get("annotations") is not None and m["annotations"]["vnd.docker.reference.type"] == "attestation-manifest":
        continue
    if m["platform"].get("architecture") is not None :
        arch = m["platform"]["architecture"]
        print(f"{arch}={m['digest']}")

