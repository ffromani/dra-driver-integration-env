# Copyright 2025 The Kubernetes Authors.
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

FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
ARG TARGETARCH
ARG GOARCH=${TARGETARCH} CGO_ENABLED=0

# cache go modules
WORKDIR /go/src/drv
COPY go.mod go.sum .
RUN go mod download

# build
COPY . .
RUN make build-all

# copy binary onto base image
FROM alpine:3.23
RUN apk add pciutils
COPY --from=builder --chown=root:root /go/src/drv/build/bin/dramemory /bin/dramemory
COPY --from=builder --chown=root:root /go/src/drv/build/bin/dracpu /bin/dracpu
COPY --from=builder --chown=root:root /go/src/drv/build/bin/drasriov /bin/drasriov
COPY --from=builder --chown=root:root /go/src/drv/build/bin/setup-runtime-containerd /bin/setup-runtime-containerd
COPY --from=builder --chown=root:root /go/src/drv/build/bin/setup-runtime /bin/setup-runtime
CMD ["/bin/sleep", "inf"]
