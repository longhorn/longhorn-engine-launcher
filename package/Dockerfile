# Stage 1: build binary from go source code
FROM  registry.suse.com/bci/golang:1.23 AS gobuilder

ARG ARCH=amd64
ARG DEP_VERSION_FILE=versions.json

RUN zypper -n ref && \
    zypper update -y

RUN zypper -n addrepo --refresh https://download.opensuse.org/repositories/network:utilities/SLE_15_SP5/network:utilities.repo && \
    zypper --gpg-auto-import-keys ref

RUN zypper -n install wget jq

ENV GOLANG_ARCH_amd64=amd64 GOLANG_ARCH_arm64=arm64 GOLANG_ARCH_s390x=s390x GOLANG_ARCH=GOLANG_ARCH_${ARCH} \
    GOPATH=/go PATH=/go/bin:/usr/local/go/bin:${PATH} SHELL=/bin/bash
RUN go install golang.org/x/lint/golint@latest

COPY ${DEP_VERSION_FILE} /versions.json

# Build go-spdk-helper
RUN export GO_SPDK_HELPER_REPO=$(jq -r '.["go-spdk-helper"].repo' /versions.json) && \
    export GO_SPDK_HELPER_COMMIT_ID=$(jq -r '.["go-spdk-helper"].commit' /versions.json) && \
    cd /usr/src && \
    git clone ${GO_SPDK_HELPER_REPO} go-spdk-helper && \
    cd go-spdk-helper && \
    git checkout ${GO_SPDK_HELPER_COMMIT_ID} && \
    go build && \
    install -m 755 go-spdk-helper /usr/local/bin/go-spdk-helper

# Install grpc_health_probe
RUN GRPC_HEALTH_PROBE_DOWNLOAD_URL=$(wget -qO- https://api.github.com/repos/grpc-ecosystem/grpc-health-probe/releases/latest | jq -r '.assets[] | select(.name | test("linux.*'"${ARCH}"'"; "i")) | .browser_download_url') && \
    wget ${GRPC_HEALTH_PROBE_DOWNLOAD_URL} -O /usr/local/bin/grpc_health_probe && \
    chmod +x /usr/local/bin/grpc_health_probe

# Stage 2: build binary from c source code
FROM registry.suse.com/bci/bci-base:15.6 AS cbuilder

ARG ARCH=amd64
ARG DEP_VERSION_FILE=versions.json

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

RUN zypper -n install cmake gcc xsltproc docbook-xsl-stylesheets git python311 python311-pip patchelf fuse3-devel jq

COPY ${DEP_VERSION_FILE} /versions.json

# Build liblonghorn
RUN export LIBLONGHORN_REPO=$(jq -r '.["liblonghorn"].repo' /versions.json) && \
    export LIBLONGHORN_COMMIT_ID=$(jq -r '.["liblonghorn"].commit' /versions.json) && \
    cd /usr/src && \
    git clone ${LIBLONGHORN_REPO} && \
    cd liblonghorn && \
    git checkout ${LIBLONGHORN_COMMIT_ID} && \
    make && \
    make install

# Build TGT
RUN export TGT_REPO=$(jq -r '.["tgt"].repo' /versions.json) && \
    export TGT_COMMIT_ID=$(jq -r '.["tgt"].commit' /versions.json) && \
    cd /usr/src && \
    git clone ${TGT_REPO} && \
    cd tgt && \
    git checkout ${TGT_COMMIT_ID} && \
    make; \
    make install

# Build spdk
RUN export SPDK_REPO=$(jq -r '.["spdk"].repo' /versions.json) && \
    export SPDK_COMMIT_ID=$(jq -r '.["spdk"].commit' /versions.json) && \
    cd /usr/src && \
    git clone ${SPDK_REPO} spdk --recursive && \
    cd spdk && \
    git checkout ${SPDK_COMMIT_ID} && \
    git submodule update --init && \
    sed -i '/python3-pyelftools/d' ./scripts/pkgdep/sles.sh && \
    sed -i 's/python3-/python311-/g' ./scripts/pkgdep/sles.sh && \
    ./scripts/pkgdep.sh && \
    pip3 install -r ./scripts/pkgdep/requirements.txt && \
    if [ ${ARCH} = "amd64" ]; then \
        ./configure --target-arch=nehalem --disable-tests --disable-unit-tests --disable-examples && \
        make -j$(nproc) && \
        make install; \
    elif [ ${ARCH} = "arm64" ]; then \
        ./configure --target-arch=native --disable-tests --disable-unit-tests --disable-examples && \
        DPDKBUILD_FLAGS="-Dplatform=generic" make -j$(nproc) && \
        make install; \
    else \
        echo "Unsupported architecture: ${ARCH}"; \
        exit 1; \
    fi

# Build libjson-c-devel
RUN export LIBJSONC_REPO=$(jq -r '.["libjsonc"].repo' /versions.json) && \
    export LIBJSONC_COMMIT_ID=$(jq -r '.["libjsonc"].commit' /versions.json) && \
    cd /usr/src && \
    git clone ${LIBJSONC_REPO} && \
    cd json-c && \
    git checkout ${LIBJSONC_COMMIT_ID} && \
    mkdir .build && \
    cd .build && \
    cmake ../ && \
    make && \
    make install

# Build nvme-cli
RUN export NVME_CLI_REPO=$(jq -r '.["nvme-cli"].repo' /versions.json) && \
    export NVME_CLI_COMMIT_ID=$(jq -r '.["nvme-cli"].commit' /versions.json) && \
    cd /usr/src && \
    git clone ${NVME_CLI_REPO} && \
    cd nvme-cli && \
    git checkout ${NVME_CLI_COMMIT_ID} && \
    meson setup --force-fallback-for=libnvme .build && \
    meson compile -C .build && \
    meson install -C .build

# Stage 3: copy binaries to release image
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

COPY package/bin/longhorn-instance-manager /usr/local/bin/
COPY package/instance-manager /usr/local/bin/
COPY package/instance-manager-v2-prestop /usr/local/bin/

# Verify the dependencies for the binaries
RUN ldd /usr/local/bin/* /usr/local/sbin/* /usr/sbin/* | grep "not found" && exit 1 || true

# Add Tini
ENV TINI_VERSION v0.19.0
ADD https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-${ARCH} /tini
RUN chmod +x /tini
ENTRYPOINT ["/tini", "--"]

CMD ["longhorn"]
