#!/usr/bin/env bash

declare -A sysbox_shas
sysbox_shas["linux/amd64"]="f02ffb48eae99d6c884c9aa0378070cc716d028f58e87deec5ae00a41b706fe8"
sysbox_shas["linux/arm64"]="d9267eb176190b96dcfa29ba4c4c685a26a4a1aca1d7f15deb31ec33ed63de15"

ARCH="${ARCH:-linux/amd64}"
printf "%s" "${sysbox_shas[$ARCH]}"
