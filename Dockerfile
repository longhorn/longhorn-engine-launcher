# Stage 1: base environment for builders and dapper
FROM registry.suse.com/bci/golang:1.23 AS builder_base

ARG DAPPER_HOST_ARCH=${BUILDARCH}
ARG ARCH=${DAPPER_HOST_ARCH}
ARG http_proxy
ARG https_proxy

ENV HOST_ARCH=${DAPPER_HOST_ARCH} ARCH=${ARCH}
ENV DAPPER_DOCKER_SOCKET true
ENV DAPPER_ENV TAG REPO DRONE_REPO DRONE_PULL_REQUEST DRONE_COMMIT_REF
ENV DAPPER_OUTPUT bin coverage.out
ENV DAPPER_RUN_ARGS --privileged --tmpfs /go/src/github.com/longhorn/longhorn-engine/integration/.venv:exec --tmpfs /go/src/github.com/longhorn/longhorn-engine/integration/.tox:exec -v /dev:/host/dev -v /proc:/host/proc
ENV DAPPER_SOURCE /go/src/github.com/longhorn/longhorn-instance-manager

ENV GOLANGCI_LINT_VERSION="v1.60.3"

WORKDIR ${DAPPER_SOURCE}

ENTRYPOINT ["./scripts/entry"]
CMD ["ci"]


RUN zypper -n ref && \
    zypper update -y

RUN zypper refresh && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/system:/snappy/SLE_15/system:snappy.repo && \
    zypper -n --gpg-auto-import-keys ref

# Install packages
RUN zypper -n install cmake wget curl git less file \
    libglib-2_0-0 libkmod-devel libnl3-devel linux-glibc-devel pkg-config \
    psmisc tox qemu-tools fuse python3-devel git zlib-devel zlib-devel-static \
    bash-completion rdma-core-devel libibverbs xsltproc docbook-xsl-stylesheets \
    perl-Config-General libaio-devel glibc-devel-static glibc-devel iptables libltdl7 libdevmapper1_03 iproute2 jq docker gcc gcc-c++ && \
    rm -rf /var/cache/zypp/*

# Install golanci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin ${GOLANGCI_LINT_VERSION}

# Docker Builx: The docker version in dapper is too old to have buildx. Install it manually.
RUN curl -sSfLO https://github.com/docker/buildx/releases/download/v0.13.1/buildx-v0.13.1.linux-${ARCH} && \
    chmod +x buildx-v0.13.1.linux-${ARCH} && \
    mv buildx-v0.13.1.linux-${ARCH} /usr/local/bin/buildx

# Install libqcow to resolve error:
#   vendor/github.com/longhorn/longhorn-engine/pkg/qcow/libqcow.go:6:11: fatal error: libqcow.h: No such file or directory
RUN curl -sSfL https://s3-us-west-1.amazonaws.com/rancher-longhorn/libqcow-alpha-20181117.tar.gz | tar xvzf - -C /usr/src && \
    cd /usr/src/libqcow-20181117 && \
    ./configure && \
    make -j$(nproc) && \
    make install && \
    ldconfig

# Stage 3: build binary from go source code
FROM builder_base AS app_builder

WORKDIR /app

# Copy the build script and source code
COPY . /app

# Make the build script executable
RUN chmod +x /app/scripts/build

# Run the build script
RUN /app/scripts/build

# Stage 3: build binary from go source code
FROM  registry.suse.com/bci/golang:1.23 AS gobuilder

ARG ARCH=amd64
ARG BRANCH=main

RUN zypper -n ref && \
    zypper update -y

RUN zypper -n addrepo --refresh https://download.opensuse.org/repositories/network:utilities/SLE_15_SP5/network:utilities.repo && \
    zypper --gpg-auto-import-keys ref

RUN zypper -n install wget jq

ENV GOLANG_ARCH_amd64=amd64 GOLANG_ARCH_arm64=arm64 GOLANG_ARCH_s390x=s390x GOLANG_ARCH=GOLANG_ARCH_${ARCH} \
    GOPATH=/go PATH=/go/bin:/usr/local/go/bin:${PATH} SHELL=/bin/bash
RUN go install golang.org/x/lint/golint@latest

RUN git clone https://github.com/longhorn/dep-versions.git -b ${BRANCH} /usr/src/dep-versions

# Build go-spdk-helper
RUN export REPO_OVERRIDE="" && \
    export COMMIT_ID_OVERRIDE="" && \
    bash /usr/src/dep-versions/scripts/build-go-spdk-helper.sh "${REPO_OVERRIDE}" "${COMMIT_ID_OVERRIDE}"

# Install grpc_health_probe
RUN export GRPC_HEALTH_PROBE_DOWNLOAD_URL=$(wget -qO- https://api.github.com/repos/grpc-ecosystem/grpc-health-probe/releases/latest | jq -r '.assets[] | select(.name | test("linux.*'"${ARCH}"'"; "i")) | .browser_download_url') && \
    wget ${GRPC_HEALTH_PROBE_DOWNLOAD_URL} -O /usr/local/bin/grpc_health_probe && \
    chmod +x /usr/local/bin/grpc_health_probe

# Stage 4: build binary from c source code
FROM registry.suse.com/bci/bci-base:15.6 AS cbuilder

ARG ARCH=amd64
ARG BRANCH=main

RUN zypper -n ref && \
    zypper update -y

RUN zypper -n addrepo --refresh https://download.opensuse.org/repositories/system:/snappy/SLE_15/system:snappy.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/network:/utilities/SLE_15/network:utilities.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:libraries:c_c++/15.6/devel:libraries:c_c++.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:languages:python:Factory/15.6/devel:languages:python:Factory.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:languages:python:backports/SLE_15/devel:languages:python:backports.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:tools:building/15.6/devel:tools:building.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/filesystems/15.6/filesystems.repo && \
    zypper --gpg-auto-import-keys ref

RUN zypper -n install cmake gcc xsltproc docbook-xsl-stylesheets git python311 python311-pip patchelf fuse3-devel jq wget

RUN git clone https://github.com/longhorn/dep-versions.git -b ${BRANCH} /usr/src/dep-versions

# Build liblonghorn
RUN export REPO_OVERRIDE="" && \
    export COMMIT_ID_OVERRIDE="" && \
    bash /usr/src/dep-versions/scripts/build-liblonghorn.sh "${REPO_OVERRIDE}" "${COMMIT_ID_OVERRIDE}"

# Build TGT
RUN export REPO_OVERRIDE="" && \
    export COMMIT_ID_OVERRIDE="" && \
    bash /usr/src/dep-versions/scripts/build-tgt.sh "${REPO_OVERRIDE}" "${COMMIT_ID_OVERRIDE}"

# Build spdk
RUN export REPO_OVERRIDE="" && \
    export COMMIT_ID_OVERRIDE="" && \
    bash /usr/src/dep-versions/scripts/build-spdk.sh "${REPO_OVERRIDE}" "${COMMIT_ID_OVERRIDE}" "${ARCH}"

# Build libjson-c-devel
RUN export REPO_OVERRIDE="" && \
    export COMMIT_ID_OVERRIDE="" && \
    bash /usr/src/dep-versions/scripts/build-libjsonc.sh "${REPO_OVERRIDE}" "${COMMIT_ID_OVERRIDE}"

# Build nvme-cli
RUN export REPO_OVERRIDE="" && \
    export COMMIT_ID_OVERRIDE="" && \
    bash /usr/src/dep-versions/scripts/build-nvme-cli.sh "${REPO_OVERRIDE}" "${COMMIT_ID_OVERRIDE}"

# Stage 5: copy binaries to release image
FROM registry.suse.com/bci/bci-base:15.6 AS release

ARG ARCH=amd64

RUN zypper -n ref && \
    zypper update -y

RUN zypper -n addrepo --refresh https://download.opensuse.org/repositories/system:/snappy/SLE_15/system:snappy.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/network:/utilities/SLE_15/network:utilities.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:libraries:c_c++/15.6/devel:libraries:c_c++.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:languages:python:Factory/15.6/devel:languages:python:Factory.repo && \
    zypper -n addrepo --refresh https://download.opensuse.org/repositories/devel:languages:python:backports/SLE_15/devel:languages:python:backports.repo && \
    zypper --gpg-auto-import-keys ref

RUN zypper -n install nfs-client nfs4-acl-tools cifs-utils sg3_utils \
    iproute2 qemu-tools e2fsprogs xfsprogs util-linux-systemd python311-base libcmocka-devel device-mapper netcat kmod jq util-linux procps fuse3-devel awk && \
    rm -rf /var/cache/zypp/*

# Install SPDK dependencies
COPY --from=cbuilder /usr/src/spdk/scripts /usr/src/spdk/scripts
COPY --from=cbuilder /usr/src/spdk/include /usr/src/spdk/include
RUN bash /usr/src/spdk/scripts/pkgdep.sh

# Copy pre-built binaries from cbuilder and gobuilder
COPY --from=gobuilder \
    /usr/local/bin/grpc_health_probe \
    /usr/local/bin/go-spdk-helper \
    /usr/local/bin/

COPY --from=cbuilder \
    /usr/local/bin/spdk_* \
    /usr/local/bin/

COPY --from=cbuilder \
    /usr/local/sbin/nvme \
    /usr/local/sbin/

COPY --from=cbuilder \
    /usr/sbin/tgt-admin \
    /usr/sbin/tgt-setup-lun \
    /usr/sbin/tgtadm \
    /usr/sbin/tgtd \
    /usr/sbin/tgtimg \
    /usr/sbin/

COPY --from=cbuilder \
   /usr/local/lib64 \
   /usr/local/lib64

RUN ldconfig

COPY --from=app_builder /app/bin/longhorn-instance-manager /usr/local/bin/
COPY --from=app_builder /app/package/instance-manager /usr/local/bin/
COPY --from=app_builder /app/package/instance-manager-v2-prestop /usr/local/bin/

# Verify the dependencies for the binaries
RUN ldd /usr/local/bin/* /usr/local/sbin/* /usr/sbin/* | grep "not found" && exit 1 || true

# Add Tini
ENV TINI_VERSION v0.19.0
ADD https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-${ARCH} /tini
RUN chmod +x /tini
ENTRYPOINT ["/tini", "--"]

CMD ["longhorn"]
