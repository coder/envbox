#!/usr/bin/env bash

set -euo pipefail

PROJECT_ROOT=$(git rev-parse --show-toplevel)

curl_flags=(
  --silent
  --show-error
  --location
)

gpg_flags=(
  --dearmor
  --yes
)

pushd "$PROJECT_ROOT/product/coder/deploy/images/envbox/files/usr/share/keyrings" >/dev/null 2>&1
  # Upstream Docker signing key
  curl "${curl_flags[@]}" "https://download.docker.com/linux/ubuntu/gpg" | \
    gpg "${gpg_flags[@]}" --output="docker.gpg"
popd >/dev/null 2>&1
