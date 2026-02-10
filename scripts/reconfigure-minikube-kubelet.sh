#!/bin/bash

set -x
set -e

sed -i 's|containerRuntimeEndpoint: unix:///var/run/cri-dockerd.sock|containerRuntimeEndpoint: unix:///run/containerd/containerd.sock|'g /var/lib/kubelet/config.yaml
docker images --format '{{.Repository}}:{{.Tag}}' | grep -v '<none>' | while read image; do
  echo "Moving $image..."
  docker save "$image" | ctr -n k8s.io images import -
done
