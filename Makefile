# Copyright 2025 Kubernetes Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

REPO_ROOT:=${CURDIR}
OUT_DIR=$(REPO_ROOT)/build
DEPS_DIR=$(OUT_DIR)/deps
BIN_DIR=$(OUT_DIR)/bin
YAML_DIR=$(OUT_DIR)/yaml

# platform on which we run
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)
# target platform(s)
PLATFORMS?=linux/amd64
CONTAINER_ENGINE?=docker

# disable CGO by default for static binaries
CGO_ENABLED=0
export GOROOT GO111MODULE CGO_ENABLED

# disable CGO by default for static binaries
CGO_ENABLED=0
export GOROOT GO111MODULE CGO_ENABLED

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# get image name from directory we're building
CLUSTER_NAME=dra-core-resource-drivers
IMAGE_NAME=dra-core-resource-drivers
# this is an intentionally non-existent registry to be used only by local CI using the local image loading
REGISTRY := dev.kind.local/dra
FULL_IMAGE_NAME := ${REGISTRY}/${IMAGE_NAME}
# tag based on date-sha
GIT_VERSION := $(shell date +v%Y%m%d)-$(shell git rev-parse --short HEAD)
TAG ?= $(GIT_VERSION)
# the full image tag
IMAGE := ${FULL_IMAGE_NAME}:${TAG}

# dependencies
## versions
YQ_VERSION ?= 4.47.1
# paths
YQ = $(DEPS_DIR)/yq

##@ general

default: build-all ## Default builds

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-32s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ binaries

build-all: build-driver-all build-setup-all build-validate-all ## build all the binaries

build-driver-all: build-driver-cpu build-driver-memory build-driver-sriov ## build all the drivers

build-driver-cpu: ## build the dracpu driver
	go build -v -o "$(BIN_DIR)/dracpu" ./driver/cpu

build-driver-memory: ## build the dramemory driver
	go build -v -o "$(BIN_DIR)/dramemory" ./driver/memory

build-driver-sriov: ## build the sriov driver
	go build -v -o "$(BIN_DIR)/drasriov" ./driver/sriov

build-setup-all: build-setup-runtime build-setup-runtime-containerd ## build all the setup helpers

build-setup-runtime: build-setup-runtime-containerd ## build the runtime setup entry point
	$(BIN_DIR)/setup-runtime-containerd -script > "$(BIN_DIR)/setup-runtime" && chmod 0755 "$(BIN_DIR)/setup-runtime"

build-setup-runtime-containerd: ## build the containerd setup helper
	go build -v -o "$(BIN_DIR)/setup-runtime-containerd" ./setup/containerd

build-validate-all: build-validate-alignment ## build all the validation tools

build-validate-alignment: ## build alignment validation tool
	go build -v -o "$(BIN_DIR)/validate-alignment" ./validation/alignment

##@ container images

image-all: image-drivers ## build all the container images

image-drivers: ## build the all-in-one container image with all the DRA drivers
	${CONTAINER_ENGINE} build . \
		--platform="${PLATFORMS}" \
		--tag="${IMAGE}"

##@ manifests

manifest-all: manifest-cluster manifest-cpu manifest-memory ## build all the manifests

manifest-cluster: $(YAML_DIR) ## create the cluster setup manifests
	@cp manifests/cluster/kind.yaml $(YAML_DIR)/cluster.yaml

RESERVED_CPUS ?= 0
manifest-cpu: $(YAML_DIR) manifests/cpu/install.tmpl.yaml dep-install-yq ## create the CPU driver install manifests
	@cd manifests/cpu && $(YQ) e -s '(.kind | downcase) + "-" + .metadata.name + ".part.yaml"' ../../manifests/cpu/install.tmpl.yaml
	@# need to make kind load docker-image working as expected: see https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
	@$(YQ) -i '.spec.template.spec.initContainers[0].name = "setup-runtime"' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.initContainers[0].imagePullPolicy = "IfNotPresent"' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.initContainers[0].image = "${IMAGE}"' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.initContainers[0].command = ["/bin/setup-runtime"]' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.containers[0].imagePullPolicy = "IfNotPresent"' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.containers[0].image = "${IMAGE}"' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.containers[0].command = ["/bin/dracpu"]' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.spec.containers[0].args = ["-v=6", "--cpu-device-mode=grouped", "--reserved-cpus=${RESERVED_CPUS}"]' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) -i '.spec.template.metadata.labels["build"] = "${GIT_VERSION}"' manifests/cpu/daemonset-dracpu.part.yaml
	@$(YQ) '.' \
		manifests/cpu/clusterrole-dracpu.part.yaml \
		manifests/cpu/serviceaccount-dracpu.part.yaml \
		manifests/cpu/clusterrolebinding-dracpu.part.yaml \
		manifests/cpu/daemonset-dracpu.part.yaml \
		manifests/cpu/deviceclass-dra.cpu.part.yaml \
		> $(YAML_DIR)/install.cpu.yaml
	@rm manifests/cpu/*.part.yaml

manifest-memory: $(YAML_DIR) manifests/memory/install.tmpl.yaml dep-install-yq ## create the memory driver install manifests
	@cd manifests/memory && $(YQ) e -s '(.kind | downcase) + "-" + .metadata.name + ".part.yaml"' ../../manifests/memory/install.tmpl.yaml
	@# need to make kind load docker-image working as expected: see https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
	@$(YQ) -i '.spec.template.spec.initContainers[0].imagePullPolicy = "IfNotPresent"' manifests/memory/daemonset-dramemory.part.yaml
	@$(YQ) -i '.spec.template.spec.initContainers[0].image = "${IMAGE}"' manifests/memory/daemonset-dramemory.part.yaml
	@$(YQ) -i '.spec.template.spec.containers[0].imagePullPolicy = "IfNotPresent"' manifests/memory/daemonset-dramemory.part.yaml
	@$(YQ) -i '.spec.template.spec.containers[0].image = "${IMAGE}"' manifests/memory/daemonset-dramemory.part.yaml
	@$(YQ) -i '.spec.template.metadata.labels["build"] = "${GIT_VERSION}"' manifests/memory/daemonset-dramemory.part.yaml
	@$(YQ) '.' \
		manifests/memory/clusterrole-dramemory.part.yaml \
		manifests/memory/serviceaccount-dramemory.part.yaml \
		manifests/memory/clusterrolebinding-dramemory.part.yaml \
		manifests/memory/daemonset-dramemory.part.yaml \
		manifests/memory/deviceclass-dra.memory.part.yaml \
		manifests/memory/deviceclass-dra.hugepages-1g.part.yaml \
		manifests/memory/deviceclass-dra.hugepages-2m.part.yaml \
		> $(YAML_DIR)/install.memory.yaml
	@rm manifests/memory/*.part.yaml

##@ kind management

kind-setup: kind-create kind-install ## setup the test cluster from scratch

kind-create: image-all ## create and preload a kind cluster from scratch
	kind create cluster --name ${CLUSTER_NAME} --config build/yaml/cluster.yaml
	kubectl label node ${CLUSTER_NAME}-worker node-role.kubernetes.io/worker=''
	kind load docker-image --name ${CLUSTER_NAME} ${IMAGE}
	scripts/wait-worker-nodes.sh

kind-install: manifest-all ## install the DRA drivers on the default cluster
	kubectl create -f build/yaml/install.cpu.yaml
	kubectl create -f build/yaml/install.memory.yaml
	# TODO: sriov
	scripts/wait-resourcelices.sh

kind-teardown: ## teardown the purpose-built test cluster
	kind delete cluster --name ${CLUSTER_NAME}

##@ dependencies

.PHONY:
dep-install-yq: $(DEPS_DIR) ## make sure the yq tool is available locally
	@# TODO: generalize platform/os?
	@if [ ! -f $(YQ) ]; then\
	       curl -L https://github.com/mikefarah/yq/releases/download/v$(YQ_VERSION)/yq_$(OS)_$(ARCH) -o $(YQ);\
               chmod 0755 $(YQ);\
	fi

# utilities (intentionally plain comments)

$(YAML_DIR): $(OUT_DIR) ## creates the yaml output directory (used internally)
	@mkdir -p $(YAML_DIR)

$(DEPS_DIR): $(OUT_DIR) ## creates the dependencies directory (used internally)
	@mkdir -p $(DEPS_DIR)

$(OUT_DIR):  ## creates the output directory (used internally)
	@mkdir -p $(OUT_DIR)

