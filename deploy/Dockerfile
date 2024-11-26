# Ubuntu 22.04 LTS (Jammy Jellyfish)
FROM ubuntu:jammy

ARG TARGETARCH
# This should be updated in the Makefile whenever the version is changed. 
# We don't hardcode it here because we have to be able to build both
# amd and arm
ARG SYSBOX_SHA
ARG SYSBOX_VERSION="0.6.5"
ARG SYSBOX_DEB="sysbox-ce_$SYSBOX_VERSION-0.linux_$TARGETARCH.deb"

# Copy configuration files to appropriate locations
COPY files /

LABEL \
  org.opencontainers.image.title="Envbox" \
  org.opencontainers.image.url="https://github.com/coder/envbox" \
  org.opencontainers.image.source="https://github.com/coder/envbox" \
  org.opencontainers.image.description="Run Docker in Docker in Kubernetes"

# Basic utilities
ARG DEBIAN_FRONTEND=noninteractive
# Pin docker to avoid any breaking API changes between the Go client and 
# the server.
ARG DOCKER_VERSION="5:27.3.1-1~ubuntu.22.04~jammy"
#   Ignore other repositories, as some require HTTPS
RUN apt-get update --quiet --option Dir::Etc::SourceParts="" && \
    apt-get upgrade -y && \
    apt-get install --no-install-recommends --yes --quiet --option Dir::Etc::SourceParts="" \
      apt-transport-https \
      apt-utils \
      binutils \
      ca-certificates \
      curl \
      dialog \
      fuse \
      iproute2 \
      jq \
      kmod \
      lsb-release \
      make \
      mokutil  \
      rsync \
      systemctl \
      wget \
      vim && \
    # Install packages from third-party repositories
    apt-get update --quiet && \
    apt-get install --no-install-recommends --yes --quiet \
      containerd.io \
      docker-ce=$DOCKER_VERSION \
      docker-ce-cli=$DOCKER_VERSION && \
    # Delete package cache to avoid consuming space in layer
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

RUN wget https://downloads.nestybox.com/sysbox/releases/v$SYSBOX_VERSION/$SYSBOX_DEB && \
  echo "$SYSBOX_SHA $SYSBOX_DEB" | sha256sum --check --status && \
  apt install -y "./$SYSBOX_DEB" && \
  rm "./$SYSBOX_DEB" && \
  userdel -r sysbox

# This is jank but sysbox adds their own /etc/docker/daemon.json that overwrites ours when it gets installed,
# so we copy over their changes to get the configurations we actually want.
COPY files /

# Add coder user
RUN useradd coder \
      --create-home \
      --shell=/bin/bash \
      --groups=docker \
      --uid=1000 \
      --user-group && \
    usermod coder \
      --add-subuids 100000-165535 \
      --add-subgids 100000-165535

# Do this last so hotswapping is fast!
ARG ENVBOX_BIN=envbox
COPY $ENVBOX_BIN /

CMD ["/envbox"]
