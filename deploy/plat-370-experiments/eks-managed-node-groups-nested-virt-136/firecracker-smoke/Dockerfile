FROM debian:bookworm-slim AS build

ARG FIRECRACKER_VERSION=1.15.1
ARG FIRECRACKER_CI_VERSION=v1.9

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tar \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /tmp/build

RUN set -eux; \
    curl -fsSL -o firecracker.tgz \
      "https://github.com/firecracker-microvm/firecracker/releases/download/v${FIRECRACKER_VERSION}/firecracker-v${FIRECRACKER_VERSION}-x86_64.tgz"; \
    tar -xzf firecracker.tgz; \
    find . -type f -name 'firecracker-v*-x86_64' -exec install -m 0755 {} /tmp/firecracker \;; \
    test -x /tmp/firecracker

RUN set -eux; \
    curl -fsSL -o /tmp/vmlinux \
      "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/${FIRECRACKER_CI_VERSION}/x86_64/vmlinux-6.1.102"; \
    curl -fsSL -o /tmp/rootfs.ext4 \
      "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/${FIRECRACKER_CI_VERSION}/x86_64/ubuntu-22.04.ext4"; \
    test -s /tmp/vmlinux; \
    test -s /tmp/rootfs.ext4

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl iproute2 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /tmp/firecracker /usr/local/bin/firecracker
COPY --from=build /tmp/vmlinux /var/lib/firecracker/vmlinux
COPY --from=build /tmp/rootfs.ext4 /var/lib/firecracker/rootfs.ext4
COPY firecracker-smoke/run-smoke.sh /usr/local/bin/run-smoke.sh
RUN chmod 0755 /usr/local/bin/run-smoke.sh
ENTRYPOINT ["/usr/local/bin/run-smoke.sh"]
